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
import { Input as InputPrimitive } from '@base-ui/react/input'
import * as React from 'react'

import { cn } from '@/lib/utils'

// Autofill is off unless a field asks for it: only the sign-in form declares
// username / current-password, so saved logins never land in settings fields.
// Browsers ignore autocomplete="off" on password fields and fill the saved
// login into them and the text field before them; new-password stops that.
function Input({
  className,
  type,
  autoComplete,
  ...props
}: React.ComponentProps<'input'>) {
  let resolvedAutoComplete = autoComplete
  if (!autoComplete || autoComplete === 'off') {
    resolvedAutoComplete = type === 'password' ? 'new-password' : 'off'
  }
  return (
    <InputPrimitive
      type={type}
      autoComplete={resolvedAutoComplete}
      data-slot='input'
      className={cn(
        'border-input file:text-foreground placeholder:text-muted-foreground focus-visible:border-ring focus-visible:ring-ring/50 disabled:bg-input/50 aria-invalid:border-destructive aria-invalid:ring-destructive/20 dark:bg-input/30 dark:disabled:bg-input/80 dark:aria-invalid:border-destructive/50 dark:aria-invalid:ring-destructive/40 h-8 w-full min-w-0 rounded-lg border bg-transparent px-2.5 py-1 text-base transition-colors outline-none file:inline-flex file:h-6 file:border-0 file:bg-transparent file:text-sm file:font-medium focus-visible:ring-3 focus-visible:ring-inset disabled:pointer-events-none disabled:cursor-not-allowed disabled:opacity-50 aria-invalid:ring-3 aria-invalid:ring-inset md:text-sm',
        className
      )}
      {...props}
    />
  )
}

export { Input }
