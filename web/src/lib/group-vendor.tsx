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
import { LayoutGrid } from 'lucide-react'
import type { ReactNode } from 'react'

import { getLobeIcon } from '@/lib/lobe-icon'

// Must match setting.GroupVendorIDs on the server.
export const GROUP_VENDOR_IDS = [
  'openai',
  'anthropic',
  'domestic',
  'google',
  'xai',
  'other',
] as const

export type GroupVendorId = (typeof GROUP_VENDOR_IDS)[number]

type GroupVendorDefinition = {
  id: GroupVendorId
  labelKey: string
  icons: string[]
}

export const GROUP_VENDORS: readonly GroupVendorDefinition[] = [
  { id: 'openai', labelKey: 'OpenAI', icons: ['OpenAI'] },
  { id: 'anthropic', labelKey: 'Anthropic', icons: ['Claude.Color'] },
  {
    id: 'domestic',
    labelKey: 'Domestic models',
    icons: ['DeepSeek.Color', 'Kimi.Color'],
  },
  { id: 'google', labelKey: 'Google', icons: ['Gemini.Color'] },
  { id: 'xai', labelKey: 'xAI', icons: ['XAI'] },
  { id: 'other', labelKey: 'Other', icons: [] },
]

export function isGroupVendorId(value: unknown): value is GroupVendorId {
  return GROUP_VENDOR_IDS.some((id) => id === value)
}

export function getGroupVendor(id: GroupVendorId): GroupVendorDefinition {
  return GROUP_VENDORS.find((vendor) => vendor.id === id) ?? GROUP_VENDORS[5]
}

export function renderGroupVendorIcons(
  id: GroupVendorId,
  size: number
): ReactNode {
  const vendor = getGroupVendor(id)
  if (vendor.icons.length === 0) {
    return (
      <LayoutGrid aria-hidden='true' style={{ width: size, height: size }} />
    )
  }
  return (
    <span className='inline-flex items-center gap-1' aria-hidden='true'>
      {vendor.icons.map((icon) => (
        <span key={icon} className='inline-flex'>
          {getLobeIcon(icon, size)}
        </span>
      ))}
    </span>
  )
}
