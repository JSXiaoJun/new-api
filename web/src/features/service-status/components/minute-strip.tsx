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
import { useTranslation } from 'react-i18next'

import {
  formatUptimePct,
  getSuccessRateDotClass,
} from '@/features/performance-metrics/lib/format'
import dayjs from '@/lib/dayjs'
import { cn } from '@/lib/utils'

import type { MinuteSlot } from '../lib/minutes'

interface MinuteStripProps {
  slots: MinuteSlot[]
  label: string
  className?: string
}

export function MinuteStrip(props: MinuteStripProps) {
  const { t } = useTranslation()

  const describe = (slot: MinuteSlot): string => {
    const time = dayjs(slot.ts * 1000).format('HH:mm')
    if (slot.state === 'idle') return `${time} · ${t('No requests')}`
    return `${time} · ${t('Success rate')} ${formatUptimePct(slot.successRate)}`
  }

  const slotClass = (slot: MinuteSlot): string => {
    if (slot.state === 'idle') return 'bg-muted'
    return getSuccessRateDotClass(slot.successRate)
  }

  return (
    <div
      role='img'
      aria-label={props.label}
      className={cn(
        'flex h-6 w-full min-w-0 items-stretch gap-px',
        props.className
      )}
    >
      {props.slots.map((slot) => (
        <span
          key={slot.ts}
          aria-hidden
          title={describe(slot)}
          data-state={slot.state}
          className={cn('min-w-0 flex-1 rounded-[2px]', slotClass(slot))}
        />
      ))}
    </div>
  )
}
