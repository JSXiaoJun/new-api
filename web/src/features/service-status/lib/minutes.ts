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
import {
  GROUP_VENDOR_IDS,
  isGroupVendorId,
  type GroupVendorId,
} from '@/lib/group-vendor'

import type { LiveGroupStatus, LiveMinutePoint } from '../types'

export type MinuteSlot =
  | { ts: number; state: 'idle' }
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
    const successRate = byTs.get(ts)
    if (successRate == null) {
      slots.push({ ts, state: 'idle' })
      continue
    }
    slots.push({ ts, state: 'rated', successRate })
  }
  return slots
}

export const DEFAULT_STATUS_VENDOR: GroupVendorId = 'openai'

/** Groups without a vendor fall under Other, as on the API key form. */
export function getStatusGroupVendor(group: LiveGroupStatus): GroupVendorId {
  return isGroupVendorId(group.vendor) ? group.vendor : 'other'
}

/**
 * The category the page shows: the visitor's pick while it still has groups
 * (null shows every group), otherwise OpenAI, otherwise the first one.
 */
export function resolveStatusVendor(
  picked: GroupVendorId | null | undefined,
  vendors: GroupVendorId[]
): GroupVendorId | null {
  if (picked === null) return null
  if (picked !== undefined && vendors.includes(picked)) return picked
  if (vendors.includes(DEFAULT_STATUS_VENDOR)) return DEFAULT_STATUS_VENDOR
  return vendors[0] ?? null
}

/** Vendor categories that have at least one reported group, in display order. */
export function getStatusVendors(groups: LiveGroupStatus[]): GroupVendorId[] {
  return GROUP_VENDOR_IDS.filter((id) =>
    groups.some((group) => getStatusGroupVendor(group) === id)
  )
}
