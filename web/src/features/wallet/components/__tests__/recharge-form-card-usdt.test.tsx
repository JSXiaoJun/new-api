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

import type { TopupInfo } from '../../types'
import { RechargeFormCard } from '../recharge-form-card'

type CardProps = ComponentProps<typeof RechargeFormCard>

const topupInfo = {
  enable_online_topup: true,
  pay_methods: [{ name: 'Alipay', type: 'alipay', min_topup: 0 }],
  min_topup: 1,
} as unknown as TopupInfo

function renderCard(overrides: Partial<CardProps>) {
  const props: CardProps = {
    topupInfo,
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

function usdtOption(selected: boolean) {
  return {
    selected,
    onSelect: vi.fn(),
    onExit: vi.fn(),
    panel: <div>usdt panel</div>,
  }
}

describe('RechargeFormCard USDT payment method', () => {
  it('shows a USDT tile next to the other methods and switches to it when clicked', async () => {
    const user = userEvent.setup()
    const usdt = usdtOption(false)
    renderCard({ usdt })

    expect(screen.getByLabelText('Custom Amount')).toBeInTheDocument()
    expect(screen.queryByText('usdt panel')).not.toBeInTheDocument()
    await user.click(screen.getByRole('button', { name: /USDT/ }))
    expect(usdt.onSelect).toHaveBeenCalledTimes(1)
  })

  it('replaces the amount form with the USDT panel while USDT is selected', () => {
    renderCard({ usdt: usdtOption(true) })

    expect(screen.getByText('usdt panel')).toBeInTheDocument()
    expect(screen.queryByLabelText('Custom Amount')).not.toBeInTheDocument()
    expect(screen.getByRole('button', { name: /USDT/ })).toHaveAttribute(
      'aria-pressed',
      'true'
    )
  })

  it('switches back instead of paying when another method is clicked in USDT mode', async () => {
    const user = userEvent.setup()
    const usdt = usdtOption(true)
    const onPaymentMethodSelect = vi.fn()
    renderCard({ usdt, onPaymentMethodSelect })

    await user.click(screen.getByRole('button', { name: 'Alipay' }))
    expect(usdt.onExit).toHaveBeenCalledTimes(1)
    expect(onPaymentMethodSelect).not.toHaveBeenCalled()
  })

  it('opens the USDT panel directly when USDT is the only online method', () => {
    renderCard({
      topupInfo: { enable_online_topup: false } as unknown as TopupInfo,
      usdt: usdtOption(false),
    })

    expect(screen.getByText('usdt panel')).toBeInTheDocument()
    expect(screen.getByRole('button', { name: /USDT/ })).toHaveAttribute(
      'aria-pressed',
      'true'
    )
  })

  it('pays with the clicked method as before when USDT is not selected', async () => {
    const user = userEvent.setup()
    const onPaymentMethodSelect = vi.fn()
    renderCard({ usdt: usdtOption(false), onPaymentMethodSelect })

    await user.click(screen.getByRole('button', { name: 'Alipay' }))
    expect(onPaymentMethodSelect).toHaveBeenCalledTimes(1)
  })
})
