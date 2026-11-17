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
import type { ComponentProps } from 'react'
import { describe, expect, it, vi } from 'vitest'

import { PaymentConfirmDialog } from '../dialogs/payment-confirm-dialog'

type DialogProps = ComponentProps<typeof PaymentConfirmDialog>

function renderDialog(overrides: Partial<DialogProps>) {
  const props: DialogProps = {
    open: true,
    onOpenChange: vi.fn(),
    onConfirm: vi.fn(),
    topupAmount: 10,
    paymentAmount: 10,
    paymentMethod: { name: 'Alipay', type: 'alipay' },
    calculating: false,
    processing: false,
    ...overrides,
  }
  return render(<PaymentConfirmDialog {...props} />)
}

describe('PaymentConfirmDialog payment fee', () => {
  it('shows the included fee when the order is below the fee threshold', () => {
    renderDialog({ paymentAmount: 10.06, feeAmount: 0.06 })

    expect(screen.getByText('Includes payment fee')).toBeInTheDocument()
    expect(screen.getByText('0.06')).toBeInTheDocument()
    expect(screen.getByText('10.06')).toBeInTheDocument()
  })

  it('hides the fee line when the merchant bears the fee', () => {
    renderDialog({ paymentAmount: 100, feeAmount: 0 })

    expect(screen.queryByText('Includes payment fee')).not.toBeInTheDocument()
  })

  it('computes the discount saving from the amount before the fee', () => {
    // 10 at a 0.9 discount is 9, plus a 0.06 fee.
    renderDialog({ paymentAmount: 9.06, feeAmount: 0.06, discountRate: 0.9 })

    expect(screen.getByText('You save')).toBeInTheDocument()
    expect(screen.getByText('1')).toBeInTheDocument()
    expect(screen.getByText('10.06')).toHaveClass('line-through')
  })
})
