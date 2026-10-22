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
import dayjs from '@/lib/dayjs'

import type { ChannelQuotaRank } from '../types'

export type ChannelRankSummary = {
  netQuota: number
  consumedQuota: number
  refundedQuota: number
  requestCount: number
  channelCount: number
}

/** Local calendar day of `date` as a half-open [start, end) range in seconds. */
export function getDayRange(date: Date): { start: number; end: number } {
  const start = dayjs(date).startOf('day')
  return { start: start.unix(), end: start.add(1, 'day').unix() }
}

export function summarizeChannelRank(
  rows: ChannelQuotaRank[]
): ChannelRankSummary {
  const summary: ChannelRankSummary = {
    netQuota: 0,
    consumedQuota: 0,
    refundedQuota: 0,
    requestCount: 0,
    channelCount: rows.length,
  }
  for (const row of rows) {
    summary.netQuota += row.net_quota
    summary.consumedQuota += row.consumed_quota
    summary.refundedQuota += row.refunded_quota
    summary.requestCount += row.request_count
  }
  return summary
}

/** Share of the day's net consumption in percent; null when there is nothing to share. */
export function getNetQuotaShare(
  netQuota: number,
  totalNetQuota: number
): number | null {
  if (totalNetQuota <= 0) return null
  return (netQuota / totalNetQuota) * 100
}
