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
import type { LiveMinutePoint } from '../types'

export type MinuteSlot =
  | { ts: number; state: 'idle' }
  | { ts: number; state: 'sparse' }
  | { ts: number; state: 'rated'; successRate: number }

/**
 * Lays the reported minutes onto every minute of the window, oldest first,
 * so minutes without requests still take their place on the strip.
 */
export function buildMinuteSlots(
  windowStart: number,
  windowEnd: number,
  bucketSeconds: number,
  points: LiveMinutePoint[]
): MinuteSlot[] {
  if (bucketSeconds <= 0 || windowEnd < windowStart) return []
  const byTs = new Map(points.map((point) => [point.ts, point.success_rate]))
  const slots: MinuteSlot[] = []
  for (let ts = windowStart; ts <= windowEnd; ts += bucketSeconds) {
    if (!byTs.has(ts)) {
      slots.push({ ts, state: 'idle' })
      continue
    }
    const successRate = byTs.get(ts)
    if (successRate == null) {
      slots.push({ ts, state: 'sparse' })
      continue
    }
    slots.push({ ts, state: 'rated', successRate })
  }
  return slots
}
