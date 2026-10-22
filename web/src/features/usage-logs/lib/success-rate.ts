/*
Copyright (C) 2023-2026 QuantumNous

This program is free software: you can redistribute it and/or modify
it under the terms of the GNU Affero General Public License as
published by the Free Software Foundation, either version 3 of the
License, or (at your option) any later version.

This program is distributed in the hope that it will be useful,
but WITHOUT ANY WARRANTY; without even the implied warranty of
MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE. See the
GNU Affero General Public License for more details.

You should have received a copy of the GNU Affero General Public License
along with this program. If not, see <https://www.gnu.org/licenses/>.

For commercial licensing, please contact support@quantumnous.com
*/
import { LOG_TYPE_ENUM } from '../constants'
import type { UsageLog } from '../data/schema'
import { parseLogOther } from './format'

/** Platform success rates are kept only for this long. */
export const SUCCESS_RATE_RETENTION_SECONDS = 24 * 60 * 60

/** Success rate of all requests in the window around one failed log. */
export type PlatformSuccessRate = {
  rate: number
  windowMinutes: number
}

export type SuccessRatePoint = {
  created_at: number
  group: string
  model_name: string
}

/**
 * Whether a log records a request that failed on the platform side, where
 * other users' success rate at the same time tells whether it was widespread.
 * Requests the client abandoned and safety policy blocks are not such failures.
 */
export function isPlatformFailureLog(log: UsageLog): boolean {
  if (log.type === LOG_TYPE_ENUM.ERROR) return true
  if (log.type !== LOG_TYPE_ENUM.CONSUME) return false
  const other = parseLogOther(log.other)
  if (!other?.termination || other.policy_block) return false
  return (
    other.termination.ended_by === 'upstream' ||
    other.termination.ended_by === 'gateway'
  )
}

/** Failed logs recent enough to have a platform success rate, by log id. */
export function collectSuccessRatePoints(
  logs: UsageLog[],
  nowSeconds: number
): { ids: number[]; points: SuccessRatePoint[] } {
  const ids: number[] = []
  const points: SuccessRatePoint[] = []
  for (const log of logs) {
    if (!log.model_name || !log.group) continue
    if (log.created_at < nowSeconds - SUCCESS_RATE_RETENTION_SECONDS) continue
    if (!isPlatformFailureLog(log)) continue
    ids.push(log.id)
    points.push({
      created_at: log.created_at,
      group: log.group,
      model_name: log.model_name,
    })
  }
  return { ids, points }
}
