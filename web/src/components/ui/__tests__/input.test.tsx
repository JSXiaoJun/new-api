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
import { render, screen } from '@testing-library/react'
import { expect, test } from 'vitest'

import { Input } from '../input'

test('an input without a hint turns browser autofill off', () => {
  render(<Input aria-label='Factory address' />)
  expect(screen.getByLabelText('Factory address')).toHaveAttribute(
    'autocomplete',
    'off'
  )
})

test.each([
  ['no hint', undefined],
  ['autocomplete off, which browsers ignore on password fields', 'off'],
])(
  'a password field with %s asks browsers not to fill saved logins',
  (_case, autoComplete) => {
    render(
      <Input aria-label='Secret' type='password' autoComplete={autoComplete} />
    )
    expect(screen.getByLabelText('Secret')).toHaveAttribute(
      'autocomplete',
      'new-password'
    )
  }
)

test('an input with an explicit hint keeps it', () => {
  render(
    <Input
      aria-label='Password'
      type='password'
      autoComplete='current-password'
    />
  )
  expect(screen.getByLabelText('Password')).toHaveAttribute(
    'autocomplete',
    'current-password'
  )
})
