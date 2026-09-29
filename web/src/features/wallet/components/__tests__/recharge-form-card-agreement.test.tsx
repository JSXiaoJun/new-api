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
import userEvent from '@testing-library/user-event'
import type { ComponentProps } from 'react'
import { describe, expect, it, vi } from 'vitest'

import { RechargeFormCard } from '../recharge-form-card'

type CardProps = ComponentProps<typeof RechargeFormCard>

function renderCard(overrides: Partial<CardProps>) {
  const props: CardProps = {
    topupInfo: null,
    presetAmounts: [],
    selectedPreset: null,
    onSelectPreset: vi.fn(),
    topupAmount: 10,
    onTopupAmountChange: vi.fn(),
    paymentAmount: 10,
    calculating: false,
    onPaymentMethodSelect: vi.fn(),
    paymentLoading: null,
    redemptionCode: '',
    onRedemptionCodeChange: vi.fn(),
    onRedeem: vi.fn(),
    redeeming: false,
    ...overrides,
  }
  return render(<RechargeFormCard {...props} />)
}

describe('RechargeFormCard pre-purchase confirmation button', () => {
  it('announces an unsigned agreement and opens the dialog when clicked', async () => {
    const onOpenPurchaseAgreement = vi.fn()
    const user = userEvent.setup()
    renderCard({ purchaseAgreementSigned: false, onOpenPurchaseAgreement })

    const button = screen.getByRole('button', {
      name: 'Pre-purchase Confirmation: Not confirmed',
    })
    await user.click(button)

    expect(onOpenPurchaseAgreement).toHaveBeenCalledTimes(1)
  })

  it('announces a signed agreement as confirmed', () => {
    renderCard({
      purchaseAgreementSigned: true,
      onOpenPurchaseAgreement: vi.fn(),
    })

    expect(
      screen.getByRole('button', {
        name: 'Pre-purchase Confirmation: Confirmed',
      })
    ).toBeInTheDocument()
  })

  it('hides the button when no dialog handler is provided', () => {
    renderCard({ onOpenBilling: vi.fn() })

    expect(
      screen.queryByRole('button', { name: /Pre-purchase Confirmation/ })
    ).not.toBeInTheDocument()
    expect(
      screen.getByRole('button', { name: 'Order History' })
    ).toBeInTheDocument()
  })
})
