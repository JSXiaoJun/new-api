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
  formatUptimePct,
  getSuccessRateTextClass,
} from '@/features/performance-metrics/lib/format'
import { cn } from '@/lib/utils'

interface SuccessRateValueProps {
  rate: number | null
  className?: string
}

export function SuccessRateValue(props: SuccessRateValueProps) {
  if (props.rate == null) {
    return (
      <span
        className={cn('text-muted-foreground tabular-nums', props.className)}
      >
        —
      </span>
    )
  }
  return (
    <span
      className={cn(
        'font-mono font-semibold tabular-nums',
        getSuccessRateTextClass(props.rate),
        props.className
      )}
    >
      {formatUptimePct(props.rate)}
    </span>
  )
}
