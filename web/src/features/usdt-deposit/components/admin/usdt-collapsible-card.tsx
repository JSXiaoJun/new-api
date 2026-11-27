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
import { ChevronDown } from 'lucide-react'
import { type ReactNode, useState } from 'react'

import {
  Collapsible,
  CollapsibleContent,
  CollapsibleTrigger,
} from '@/components/ui/collapsible'
import { cn } from '@/lib/utils'

/**
 * A bordered block of the USDT settings page that folds to its header line.
 * The content stays mounted while folded, so form fields keep their values
 * and validation.
 */
export function UsdtCollapsibleCard(props: {
  title: ReactNode
  /** badges and counts shown next to the title, also while folded */
  summary?: ReactNode
  defaultOpen?: boolean
  children: ReactNode
}) {
  const [open, setOpen] = useState(props.defaultOpen ?? false)
  return (
    <Collapsible
      open={open}
      onOpenChange={setOpen}
      className='rounded-lg border'
    >
      <CollapsibleTrigger
        render={
          <button
            type='button'
            className='hover:bg-muted/40 flex w-full items-center gap-3 rounded-lg px-4 py-3 text-left transition-colors'
          />
        }
      >
        <div className='flex min-w-0 flex-1 flex-wrap items-center gap-2'>
          <span className='text-sm font-medium'>{props.title}</span>
          {props.summary}
        </div>
        <ChevronDown
          className={cn(
            'text-muted-foreground size-4 shrink-0 transition-transform',
            open && 'rotate-180'
          )}
          aria-hidden='true'
        />
      </CollapsibleTrigger>
      <CollapsibleContent keepMounted className='space-y-4 px-4 pb-4'>
        {props.children}
      </CollapsibleContent>
    </Collapsible>
  )
}
