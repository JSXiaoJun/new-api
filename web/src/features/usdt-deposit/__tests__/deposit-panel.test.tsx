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
import { act, render, screen, waitFor } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import type { ReactNode } from 'react'
import { afterEach, describe, expect, it, vi } from 'vitest'

import { createUsdtAddress, getUsdtDeposits, getUsdtInfo } from '../api'
import { USDT_REFRESH_COOLDOWN_MS } from '../components/usdt-deposit-history'
import { UsdtDepositPanel } from '../components/usdt-deposit-panel'
import type { UsdtDeposit, UsdtInfo } from '../types'

vi.mock('../api', () => ({
  getUsdtInfo: vi.fn(),
  createUsdtAddress: vi.fn(),
  getUsdtDeposits: vi.fn(),
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

function deposit(id: number): UsdtDeposit {
  return {
    id,
    chain: 'bsc',
    tx_hash: `0x${String(id).padStart(64, '0')}`,
    log_index: 0,
    user_id: 1,
    address: '0xabc',
    address_display: '0xAbC',
    from_address: '0xdef',
    amount: '5',
    rate: '1',
    quota: 2500000,
    status: 'credited',
    source: 'scan',
    created_at: 1787124215,
    credited_at: 1787124220,
    tx_url: '',
  }
}

function renderCard(
  options: { onBeforeAddress?: () => boolean; onCredited?: () => void } = {}
) {
  const client = new QueryClient({
    defaultOptions: { queries: { retry: false } },
  })
  const wrapper = (props: { children: ReactNode }) => (
    <QueryClientProvider client={client}>{props.children}</QueryClientProvider>
  )
  return render(
    <UsdtDepositPanel
      onBeforeAddress={options.onBeforeAddress ?? (() => true)}
      onCredited={options.onCredited ?? vi.fn()}
    />,
    { wrapper }
  )
}

describe('UsdtDepositPanel', () => {
  afterEach(() => {
    vi.clearAllMocks()
    vi.useRealTimers()
  })

  it('says USDT is unavailable when no network is enabled', async () => {
    vi.mocked(getUsdtInfo).mockResolvedValue({ ...info({}), networks: [] })
    renderCard()
    expect(
      await screen.findByText('USDT top-up is not available')
    ).toBeInTheDocument()
    expect(
      screen.queryByRole('button', { name: 'Show my deposit address' })
    ).not.toBeInTheDocument()
  })

  it('requests the address of the selected network only after the purchase agreement check passes', async () => {
    const user = userEvent.setup()
    vi.mocked(getUsdtInfo).mockResolvedValue(info({}))
    vi.mocked(getUsdtDeposits).mockResolvedValue([])
    vi.mocked(createUsdtAddress).mockResolvedValue({
      chain: 'tron',
      address: 'T123',
    })
    // The first network is shown on load while the agreement is unsigned.
    const agreement = vi.fn().mockReturnValueOnce(false).mockReturnValue(true)
    renderCard({ onBeforeAddress: agreement })

    await user.click(
      await screen.findByRole('button', { name: /TRON \(TRC20\)/ })
    )
    await waitFor(() =>
      expect(createUsdtAddress).toHaveBeenCalledWith('tron', expect.anything())
    )
    expect(createUsdtAddress).not.toHaveBeenCalledWith('bsc', expect.anything())
  })

  it('marks a congested network and tells the user not to send again', async () => {
    const user = userEvent.setup()
    vi.mocked(getUsdtInfo).mockResolvedValue({
      ...info({ bsc: '0xb', tron: 'T1' }),
      networks: [
        { ...networks[0], status: 'normal' },
        { ...networks[1], status: 'congested' },
      ],
    })
    vi.mocked(getUsdtDeposits).mockResolvedValue([])
    renderCard()

    const bsc = await screen.findByRole('button', { name: /BSC \(BEP20\)/ })
    expect(bsc).toHaveTextContent('Smooth')
    const tron = screen.getByRole('button', { name: /TRON \(TRC20\)/ })
    expect(tron).toHaveTextContent('Congested')
    expect(screen.queryByText(/is congested right now/)).not.toBeInTheDocument()

    await user.click(tron)
    expect(
      screen.getByText(
        'TRON (TRC20) is congested right now. Deposits are still credited, but may take longer. Do not send again.'
      )
    ).toBeInTheDocument()
  })

  it('shows the permanent address with the latest deposits and no transaction hash form', async () => {
    vi.mocked(getUsdtInfo).mockResolvedValue(
      info({ bsc: '0xAbC0000000000000000000000000000000000001' })
    )
    vi.mocked(getUsdtDeposits).mockResolvedValue([deposit(1)])
    renderCard()

    expect(
      await screen.findByText('0xAbC0000000000000000000000000000000000001')
    ).toBeInTheDocument()
    expect(await screen.findByText(deposit(1).tx_hash)).toBeInTheDocument()
    expect(screen.getByText('Latest 10 only')).toBeInTheDocument()
    expect(
      screen.queryByPlaceholderText('Transaction hash (TxID)')
    ).not.toBeInTheDocument()
  })

  it('refetches deposits on refresh click and ignores further clicks until the cooldown ends', async () => {
    vi.useFakeTimers({ shouldAdvanceTime: true })
    const user = userEvent.setup({ advanceTimers: vi.advanceTimersByTime })
    vi.mocked(getUsdtInfo).mockResolvedValue(info({ bsc: '0xAbC' }))
    vi.mocked(getUsdtDeposits).mockResolvedValue([])
    renderCard()

    const refresh = await screen.findByRole('button', { name: 'Refresh' })
    act(() => {
      vi.advanceTimersByTime(1_000)
    })
    await waitFor(() => expect(refresh).toBeEnabled())
    expect(getUsdtDeposits).toHaveBeenCalledTimes(1)

    await user.click(refresh)
    await waitFor(() => expect(getUsdtDeposits).toHaveBeenCalledTimes(2))
    expect(refresh).toBeDisabled()
    await user.click(refresh)
    expect(getUsdtDeposits).toHaveBeenCalledTimes(2)

    act(() => {
      vi.advanceTimersByTime(USDT_REFRESH_COOLDOWN_MS)
    })
    await waitFor(() => expect(refresh).toBeEnabled())
  })

  it('marks the refresh button busy and disabled while the automatic refresh runs', async () => {
    vi.useFakeTimers({ shouldAdvanceTime: true })
    vi.mocked(getUsdtInfo).mockResolvedValue(info({ bsc: '0xAbC' }))
    let finish: (deposits: UsdtDeposit[]) => void = () => {}
    vi.mocked(getUsdtDeposits)
      .mockResolvedValueOnce([])
      .mockReturnValueOnce(
        new Promise((resolve) => {
          finish = resolve
        })
      )
    renderCard()

    const refresh = await screen.findByRole('button', { name: 'Refresh' })
    await waitFor(() => expect(refresh).toHaveAttribute('aria-busy', 'false'))

    act(() => {
      vi.advanceTimersByTime(15_000)
    })
    await waitFor(() => expect(refresh).toHaveAttribute('aria-busy', 'true'))
    expect(refresh).toBeDisabled()

    act(() => finish([]))
    await waitFor(() => expect(refresh).toHaveAttribute('aria-busy', 'false'))
    act(() => {
      vi.advanceTimersByTime(1_000)
    })
    await waitFor(() => expect(refresh).toBeEnabled())
  })

  it('refreshes the balance once when a refresh finds a newly credited deposit', async () => {
    const user = userEvent.setup()
    const onCredited = vi.fn()
    vi.mocked(getUsdtInfo).mockResolvedValue(info({ bsc: '0xAbC' }))
    vi.mocked(getUsdtDeposits)
      .mockResolvedValueOnce([deposit(1)])
      .mockResolvedValue([deposit(2), deposit(1)])
    renderCard({ onCredited })

    await screen.findByText(deposit(1).tx_hash)
    expect(onCredited).not.toHaveBeenCalled()

    const refresh = screen.getByRole('button', { name: 'Refresh' })
    await waitFor(() => expect(refresh).toBeEnabled(), { timeout: 2_000 })
    await user.click(refresh)
    await screen.findByText(deposit(2).tx_hash)
    expect(onCredited).toHaveBeenCalledTimes(1)
  })
})
