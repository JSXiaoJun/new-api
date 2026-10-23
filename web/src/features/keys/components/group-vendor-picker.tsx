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
import { Check } from 'lucide-react'
import { useTranslation } from 'react-i18next'

import { ToggleGroup, ToggleGroupItem } from '@/components/ui/toggle-group'
import {
  getGroupVendor,
  renderGroupVendorIcons,
  type GroupVendorId,
} from '@/lib/group-vendor'

type GroupVendorPickerProps = {
  vendors: GroupVendorId[]
  value: GroupVendorId | null
  onValueChange: (value: GroupVendorId | null) => void
}

export function GroupVendorPicker(props: GroupVendorPickerProps) {
  const { t } = useTranslation()

  return (
    <ToggleGroup
      aria-label={t('Vendor category')}
      value={props.value ? [props.value] : []}
      onValueChange={(values) => {
        const next = props.vendors.find((vendor) => values.includes(vendor))
        props.onValueChange(next ?? null)
      }}
      spacing={2}
      className='grid w-full grid-cols-3 sm:grid-cols-6'
    >
      {props.vendors.map((vendorId) => {
        const selected = props.value === vendorId
        return (
          <ToggleGroupItem
            key={vendorId}
            value={vendorId}
            variant='outline'
            className='data-[pressed]:border-primary data-[pressed]:bg-primary/5 relative h-auto min-h-20 flex-col gap-2 px-2 py-3 text-xs whitespace-normal'
          >
            {selected && (
              <span className='bg-primary text-primary-foreground absolute top-1.5 right-1.5 flex size-4 items-center justify-center rounded-full'>
                <Check aria-hidden='true' className='size-3' />
              </span>
            )}
            <span className='flex h-7 items-center'>
              {renderGroupVendorIcons(vendorId, 22)}
            </span>
            <span className='text-center leading-tight'>
              {t(getGroupVendor(vendorId).labelKey)}
            </span>
          </ToggleGroupItem>
        )
      })}
    </ToggleGroup>
  )
}
