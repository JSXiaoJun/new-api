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
import {
  createMemoryHistory,
  createRootRoute,
  createRoute,
  createRouter,
  RouterProvider,
} from '@tanstack/react-router'
import { render, screen, waitFor } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { afterEach, beforeEach, expect, it, vi } from 'vitest'

import { TooltipProvider } from '@/components/ui/tooltip'
import { api } from '@/lib/api'
import { Route as UsersRoute } from '@/routes/_authenticated/users/index'
import { useAuthStore } from '@/stores/auth-store'
import {
  DEFAULT_CURRENCY_CONFIG,
  useSystemConfigStore,
} from '@/stores/system-config-store'

import { UsersProvider } from '../users-provider'
import { UsersTable } from '../users-table'

const clients: QueryClient[] = []
// 10 USD at the default 500000 quota units per USD.
const tenUsdSearchUrl =
  '/api/user/search?keyword=&group=&min_quota=5000000&p=1&page_size=20'

beforeEach(() => {
  vi.spyOn(window, 'scrollTo').mockImplementation(() => {})
  localStorage.clear()
  useAuthStore.getState().auth.setUser({ id: 1, username: 'admin', role: 100 })
  useSystemConfigStore
    .getState()
    .setConfig({ currency: { ...DEFAULT_CURRENCY_CONFIG } })
})

afterEach(() => {
  clients.splice(0).forEach((client) => client.clear())
  localStorage.clear()
  useAuthStore.getState().auth.reset()
  useSystemConfigStore
    .getState()
    .setConfig({ currency: { ...DEFAULT_CURRENCY_CONFIG } })
  vi.restoreAllMocks()
})

async function renderUsers(initialEntry = '/users/') {
  const get = vi.spyOn(api, 'get').mockImplementation(async (url) => {
    if (url === '/api/group/') {
      return { data: { success: true, data: ['default'] } }
    }
    return {
      data: {
        success: true,
        data: {
          items: [
            {
              id: 2,
              username: 'rich-user',
              display_name: '',
              role: 1,
              status: 1,
              quota: 6000000,
              used_quota: 0,
              request_count: 0,
              group: 'default',
            },
          ],
          total: 1,
        },
      },
    }
  })
  const root = createRootRoute()
  const auth = createRoute({ getParentRoute: () => root, id: '_authenticated' })
  const users = createRoute({
    getParentRoute: () => auth,
    path: 'users/',
    validateSearch: UsersRoute.options.validateSearch,
    component: () => (
      <TooltipProvider>
        <UsersProvider>
          <UsersTable />
        </UsersProvider>
      </TooltipProvider>
    ),
  })
  const router = createRouter({
    routeTree: root.addChildren([auth.addChildren([users])]),
    history: createMemoryHistory({ initialEntries: [initialEntry] }),
  })
  const client = new QueryClient({
    defaultOptions: { queries: { retry: false } },
  })
  clients.push(client)
  await router.load()
  render(
    <QueryClientProvider client={client}>
      <RouterProvider router={router} />
    </QueryClientProvider>
  )
  await screen.findByText('rich-user')
  return { get, router }
}

it('searches users at or above the typed amount in quota units and lists all users once the field is cleared', async () => {
  const user = userEvent.setup()
  const { get, router } = await renderUsers()
  const input = screen.getByRole('textbox', { name: 'Minimum remaining quota' })

  await user.type(input, '10')

  await waitFor(() => expect(get).toHaveBeenLastCalledWith(tenUsdSearchUrl), {
    timeout: 2000,
  })
  expect(router.state.location.search).toMatchObject({ minQuota: '10' })

  await user.clear(input)

  await waitFor(
    () =>
      expect(get).toHaveBeenLastCalledWith('/api/user/', {
        params: { p: 1, page_size: 20 },
      }),
    { timeout: 2000 }
  )
  expect(router.state.location.search).not.toHaveProperty('minQuota')
})

it('converts a restored amount from the configured display currency to quota units', async () => {
  useSystemConfigStore.getState().setConfig({
    currency: {
      ...DEFAULT_CURRENCY_CONFIG,
      quotaDisplayType: 'CNY',
      usdExchangeRate: 7,
    },
  })

  const { get } = await renderUsers('/users/?minQuota=%2270%22')

  expect(get).toHaveBeenCalledWith(tenUsdSearchUrl)
  expect(
    screen.getByRole('textbox', { name: 'Minimum remaining quota' })
  ).toHaveValue('70')
})

// 1e15 USD is beyond the quota the browser can send exactly.
it.each(['1e3', '1000000000000000'])(
  'flags the amount %s and keeps the current list instead of searching',
  async (amount) => {
    const user = userEvent.setup()
    const { get, router } = await renderUsers()
    const input = screen.getByRole('textbox', {
      name: 'Minimum remaining quota',
    })

    await user.type(input, amount)

    expect(await screen.findByRole('alert')).toHaveTextContent(
      'Enter a valid amount'
    )
    expect(input).toHaveAttribute('aria-invalid', 'true')
    await waitFor(
      () =>
        expect(router.state.location.search).toMatchObject({
          minQuota: amount,
        }),
      { timeout: 2000 }
    )
    expect(screen.getByText('rich-user')).toBeInTheDocument()
    expect(get).not.toHaveBeenCalledWith(
      expect.stringContaining('/api/user/search')
    )
  }
)
