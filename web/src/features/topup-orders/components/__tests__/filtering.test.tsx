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
import { fireEvent, render, screen, waitFor } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { afterEach, beforeEach, describe, expect, test, vi } from 'vitest'

import { PageFooterProvider } from '@/components/layout/components/page-footer'
import { TooltipProvider } from '@/components/ui/tooltip'
import { api } from '@/lib/api'

import { TopUpOrdersTable } from '../topup-orders-table'

const order = {
  id: 1,
  user_id: 282,
  username: 'order-owner',
  display_name: 'Order Owner',
  email: 'owner@example.com',
  amount: 10,
  money: 10,
  trade_no: 'MATCH-order',
  payment_method: 'alipay',
  payment_provider: 'epay',
  create_time: 1787124215,
  complete_time: 1787124220,
  status: 'success',
}

describe('Recharge order filters', () => {
  let queryClient: QueryClient
  let footer: HTMLDivElement

  beforeEach(() => {
    queryClient = new QueryClient({
      defaultOptions: { queries: { retry: false } },
    })
    footer = document.createElement('div')
    document.body.append(footer)
    vi.spyOn(api, 'get').mockResolvedValue({
      data: {
        success: true,
        data: { items: [order], total: 45, page: 1, page_size: 20 },
      },
    })
    render(
      <QueryClientProvider client={queryClient}>
        <TooltipProvider>
          <PageFooterProvider container={footer}>
            <TopUpOrdersTable />
          </PageFooterProvider>
        </TooltipProvider>
      </QueryClientProvider>
    )
  })

  afterEach(() => {
    queryClient.clear()
    footer.remove()
  })

  test('changing user ID resets the page and combines with the order filter on later pages', async () => {
    const user = userEvent.setup()
    await screen.findByText('MATCH-order')
    await user.click(screen.getByRole('button', { name: 'Go to next page' }))
    await waitFor(() => {
      expect(api.get).toHaveBeenLastCalledWith('/api/user/topup', {
        params: { p: 2, page_size: 20, keyword: undefined, user_id: undefined },
      })
    })

    fireEvent.change(screen.getByRole('textbox', { name: 'User ID' }), {
      target: { value: '282' },
    })
    await waitFor(() => {
      expect(api.get).toHaveBeenLastCalledWith('/api/user/topup', {
        params: { p: 1, page_size: 20, keyword: undefined, user_id: 282 },
      })
    })

    fireEvent.change(screen.getByPlaceholderText('Search by order number...'), {
      target: { value: ' MATCH ' },
    })
    await waitFor(() => {
      expect(api.get).toHaveBeenLastCalledWith('/api/user/topup', {
        params: { p: 1, page_size: 20, keyword: 'MATCH', user_id: 282 },
      })
    })
    await user.click(screen.getByRole('button', { name: 'Go to next page' }))
    await waitFor(() => {
      expect(api.get).toHaveBeenLastCalledWith('/api/user/topup', {
        params: { p: 2, page_size: 20, keyword: 'MATCH', user_id: 282 },
      })
    })
  })

  test.each(['0', '-1', 'abc', '1.5', '999999999999999999999999'])(
    'invalid user ID %s shows validation without requesting all orders',
    async (value) => {
      await screen.findByText('MATCH-order')
      vi.mocked(api.get).mockClear()

      const input = screen.getByRole('textbox', { name: 'User ID' })
      fireEvent.change(input, { target: { value } })

      expect(await screen.findByRole('alert')).toHaveTextContent(
        'Enter a valid user ID'
      )
      expect(input).toHaveAttribute('aria-invalid', 'true')
      expect(input).toHaveAccessibleDescription('Enter a valid user ID')
      expect(api.get).not.toHaveBeenCalled()
      expect(screen.queryByText('MATCH-order')).not.toBeInTheDocument()
    }
  )

  test('reset clears both filters and returns to the first unfiltered page', async () => {
    const user = userEvent.setup()
    await screen.findByText('MATCH-order')
    fireEvent.change(screen.getByRole('textbox', { name: 'User ID' }), {
      target: { value: '282' },
    })
    fireEvent.change(screen.getByPlaceholderText('Search by order number...'), {
      target: { value: 'MATCH' },
    })
    await waitFor(() => {
      expect(api.get).toHaveBeenLastCalledWith('/api/user/topup', {
        params: { p: 1, page_size: 20, keyword: 'MATCH', user_id: 282 },
      })
    })
    await user.click(screen.getByRole('button', { name: 'Go to next page' }))
    await user.click(screen.getByRole('button', { name: 'Reset' }))

    await waitFor(() => {
      expect(api.get).toHaveBeenLastCalledWith('/api/user/topup', {
        params: { p: 1, page_size: 20, keyword: undefined, user_id: undefined },
      })
    })
    expect(screen.getByRole('textbox', { name: 'User ID' })).toHaveValue('')
    expect(
      screen.getByPlaceholderText('Search by order number...')
    ).toHaveValue('')
    expect(
      screen.getByRole('button', { name: 'Go to previous page' })
    ).toBeDisabled()
  })
})
