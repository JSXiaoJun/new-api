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
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { render, screen, waitFor } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import type { ReactNode } from 'react'
import { afterEach, describe, expect, it, vi } from 'vitest'

import {
  claimUsdtDeposit,
  createUsdtAddress,
  getUsdtDeposits,
  getUsdtInfo,
} from '../api'
import { UsdtDepositCard } from '../components/usdt-deposit-card'
import type { UsdtInfo } from '../types'

vi.mock('../api', () => ({
  getUsdtInfo: vi.fn(),
  createUsdtAddress: vi.fn(),
  getUsdtDeposits: vi.fn(),
  claimUsdtDeposit: vi.fn(),
}))

vi.mock('sonner', () => ({
  toast: { success: vi.fn(), error: vi.fn(), warning: vi.fn(), info: vi.fn() },
}))

const networks = [
  {
    id: 'bsc',
    name: 'BSC (BEP20)',
    min_amount: '1',
    contract: '0x55d3',
    tx_url: '',
  },
  {
    id: 'tron',
    name: 'TRON (TRC20)',
    min_amount: '10',
    contract: 'TR7N',
    tx_url: '',
  },
]

function info(addresses: Record<string, string>): UsdtInfo {
  return { enabled: true, networks, addresses, rate: '1', rate_mode: 'fixed' }
}

function renderCard(onBeforeAddress: () => boolean = () => true) {
  const client = new QueryClient({
    defaultOptions: { queries: { retry: false } },
  })
  const wrapper = (props: { children: ReactNode }) => (
    <QueryClientProvider client={client}>{props.children}</QueryClientProvider>
  )
  return render(
    <UsdtDepositCard onBeforeAddress={onBeforeAddress} onCredited={vi.fn()} />,
    {
      wrapper,
    }
  )
}

describe('UsdtDepositCard', () => {
  afterEach(() => {
    vi.clearAllMocks()
  })

  it('renders nothing when no network is enabled', async () => {
    vi.mocked(getUsdtInfo).mockResolvedValue({ ...info({}), networks: [] })
    const { container } = renderCard()
    await waitFor(() => expect(getUsdtInfo).toHaveBeenCalled())
    await waitFor(() => expect(container).toBeEmptyDOMElement())
  })

  it('requests the address of the selected network only after the purchase agreement check passes', async () => {
    const user = userEvent.setup()
    vi.mocked(getUsdtInfo).mockResolvedValue(info({}))
    vi.mocked(getUsdtDeposits).mockResolvedValue({
      page: 1,
      page_size: 10,
      total: 0,
      items: [],
    })
    vi.mocked(createUsdtAddress).mockResolvedValue({
      chain: 'tron',
      address: 'T123',
    })
    const agreement = vi.fn().mockReturnValueOnce(false).mockReturnValue(true)
    renderCard(agreement)

    await user.click(
      await screen.findByRole('button', { name: 'TRON (TRC20)' })
    )
    const show = screen.getByRole('button', { name: 'Show my deposit address' })
    await user.click(show)
    expect(createUsdtAddress).not.toHaveBeenCalled()

    await user.click(show)
    await waitFor(() =>
      expect(createUsdtAddress).toHaveBeenCalledWith('tron', expect.anything())
    )
  })

  it('shows the permanent address and claims a deposit only with a valid transaction hash', async () => {
    const user = userEvent.setup()
    vi.mocked(getUsdtInfo).mockResolvedValue(
      info({ bsc: '0xAbC0000000000000000000000000000000000001' })
    )
    vi.mocked(getUsdtDeposits).mockResolvedValue({
      page: 1,
      page_size: 10,
      total: 0,
      items: [],
    })
    vi.mocked(claimUsdtDeposit).mockResolvedValue([
      {
        chain: 'bsc',
        tx_hash: `0x${'a'.repeat(64)}`,
        log_index: 0,
        status: 'credited',
        amount: '5',
        quota: 2500000,
      },
    ])
    renderCard()

    expect(
      await screen.findByText('0xAbC0000000000000000000000000000000000001')
    ).toBeInTheDocument()
    const input = screen.getByPlaceholderText('Transaction hash (TxID)')
    const check = screen.getByRole('button', { name: 'Check deposit' })

    await user.type(input, '0x1234')
    expect(check).toBeDisabled()
    expect(input).toHaveAttribute('aria-invalid', 'true')

    await user.clear(input)
    await user.type(input, 'a'.repeat(64))
    expect(check).toBeEnabled()
    await user.click(check)
    await waitFor(() =>
      expect(claimUsdtDeposit).toHaveBeenCalledWith('bsc', 'a'.repeat(64))
    )
  })
})
