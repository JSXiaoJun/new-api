/*
Copyright (C) 2023-2026 QuantumNous

This program is free software: you can redistribute it and/or modify
it under the terms of the GNU Affero General Public License as published by
the Free Software Foundation, either version 3 of the License, or
(at your option) any later version.

This program is distributed in the hope that it will be useful,
but WITHOUT ANY WARRANTY; without even the implied warranty of
MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE. See the
GNU Affero General Public License for more details.

You should have received a copy of the GNU Affero General Public License
along with this program. If not, see <https://www.gnu.org/licenses/>.

For commercial licensing, please contact support@quantumnous.com
*/
import { render, screen, waitFor } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { afterEach, describe, expect, it, vi } from 'vitest'

import { cancelTopupPayment, getTopupPaymentStatus } from '../../../api'
import { WeChatPaymentDialog } from '../wechat-payment-dialog'

vi.mock('../../../api', () => ({
  cancelTopupPayment: vi.fn(),
  getTopupPaymentStatus: vi.fn(),
  isApiSuccess: (response: { success?: boolean }) => response.success === true,
}))

describe('WeChatPaymentDialog', () => {
  afterEach(() => {
    vi.clearAllMocks()
  })

  it('cancels the pending order before closing the dialog', async () => {
    vi.mocked(cancelTopupPayment).mockResolvedValue({
      success: true,
      data: { trade_no: 'WX-1', status: 'expired' },
    })
    vi.mocked(getTopupPaymentStatus).mockResolvedValue({
      success: true,
      data: { trade_no: 'WX-1', status: 'pending' },
    })
    const onOpenChange = vi.fn()
    const user = userEvent.setup()

    render(
      <WeChatPaymentDialog
        open
        onOpenChange={onOpenChange}
        payment={{ qrcode: 'https://pay.example/WX-1', trade_no: 'WX-1' }}
      />
    )

    await user.click(screen.getByRole('button', { name: 'Cancel Payment' }))

    await waitFor(() => expect(cancelTopupPayment).toHaveBeenCalledWith('WX-1'))
    expect(onOpenChange).toHaveBeenCalledWith(false)
  })

  it('settles a paid order instead of cancelling when the callback is late', async () => {
    vi.mocked(getTopupPaymentStatus).mockResolvedValue({
      success: true,
      data: { trade_no: 'WX-2', status: 'success' },
    })
    vi.mocked(cancelTopupPayment).mockResolvedValue({
      success: true,
      data: { trade_no: 'WX-2', status: 'expired' },
    })
    const onOpenChange = vi.fn()
    const onPaid = vi.fn()
    const user = userEvent.setup()

    render(
      <WeChatPaymentDialog
        open
        onOpenChange={onOpenChange}
        onPaid={onPaid}
        payment={{ qrcode: 'https://pay.example/WX-2', trade_no: 'WX-2' }}
      />
    )

    await user.click(screen.getByRole('button', { name: 'Cancel Payment' }))

    await waitFor(() => expect(onPaid).toHaveBeenCalledTimes(1))
    expect(cancelTopupPayment).not.toHaveBeenCalled()
    expect(onOpenChange).toHaveBeenCalledWith(false)
  })
})
