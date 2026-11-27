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
import { render, screen, waitFor, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import type { ReactNode } from 'react'
import { afterEach, describe, expect, it, vi } from 'vitest'

import { releaseUsdtConflict } from '../api'
import { UsdtVerificationAlerts } from '../components/admin/usdt-verification-alerts'

vi.mock('../api', () => ({ releaseUsdtConflict: vi.fn() }))
vi.mock('sonner', () => ({ toast: { success: vi.fn(), error: vi.fn() } }))

function withQuery(ui: ReactNode) {
  const client = new QueryClient({
    defaultOptions: { mutations: { retry: false } },
  })
  return <QueryClientProvider client={client}>{ui}</QueryClientProvider>
}

afterEach(() => vi.clearAllMocks())

describe('UsdtVerificationAlerts', () => {
  it('renders nothing while the verification nodes agree', () => {
    const { container } = render(
      withQuery(<UsdtVerificationAlerts alerts={[]} />)
    )
    expect(container).toBeEmptyDOMElement()
  })

  it('lists held transactions newest first with what the providers disagree on', () => {
    render(
      withQuery(
        <UsdtVerificationAlerts
          alerts={[
            {
              at: 1700000000,
              chain: 'bsc',
              tx_hash: '0xold',
              detail: 'x',
              held: false,
            },
            {
              at: 1700000100,
              chain: 'tron',
              tx_hash: '0xnew',
              detail: 'Ankr and dRPC disagree on transfers',
              held: true,
            },
          ]}
        />
      )
    )
    const alert = screen.getByRole('alert')
    expect(
      within(alert).getByText('Verification nodes disagree')
    ).toBeInTheDocument()
    const items = within(alert).getAllByRole('listitem')
    expect(items[0]).toHaveTextContent('TRON 0xnew')
    expect(items[0]).toHaveTextContent('Ankr and dRPC disagree on transfers')
    expect(items[1]).toHaveTextContent('BSC 0xold')
    expect(
      within(items[1]).queryByRole('button', { name: 'Release hold' })
    ).not.toBeInTheDocument()
  })

  it('releases a held transaction only after confirmation', async () => {
    vi.mocked(releaseUsdtConflict).mockResolvedValue(null)
    const user = userEvent.setup()
    render(
      withQuery(
        <UsdtVerificationAlerts
          alerts={[
            {
              at: 1700000100,
              chain: 'bsc',
              tx_hash: '0xheld',
              detail: 'd',
              held: true,
            },
          ]}
        />
      )
    )
    await user.click(screen.getByRole('button', { name: 'Release hold' }))
    expect(releaseUsdtConflict).not.toHaveBeenCalled()
    const dialog = await screen.findByRole('alertdialog')
    await user.click(
      within(dialog).getByRole('button', { name: 'Release hold' })
    )
    await waitFor(() =>
      expect(releaseUsdtConflict).toHaveBeenCalledWith('bsc', '0xheld')
    )
  })
})
