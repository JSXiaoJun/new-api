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
import { render, screen, within } from '@testing-library/react'
import { describe, expect, it } from 'vitest'

import { UsdtVerificationAlerts } from '../components/admin/usdt-verification-alerts'

describe('UsdtVerificationAlerts', () => {
  it('renders nothing while the verification nodes agree', () => {
    const { container } = render(<UsdtVerificationAlerts alerts={[]} />)
    expect(container).toBeEmptyDOMElement()
  })

  it('lists held transactions newest first when the nodes disagree', () => {
    render(
      <UsdtVerificationAlerts
        alerts={[
          { at: 1700000000, chain: 'bsc', tx_hash: '0xold', detail: 'x' },
          { at: 1700000100, chain: 'tron', tx_hash: '0xnew', detail: 'y' },
        ]}
      />
    )
    const alert = screen.getByRole('alert')
    expect(
      within(alert).getByText('Verification nodes disagree')
    ).toBeInTheDocument()
    const items = within(alert).getAllByRole('listitem')
    expect(items[0]).toHaveTextContent('TRON 0xnew')
    expect(items[1]).toHaveTextContent('BSC 0xold')
  })
})
