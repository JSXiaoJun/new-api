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
import {
  cleanup,
  render,
  screen,
  waitFor,
  within,
} from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'

import { TooltipProvider } from '@/components/ui/tooltip'
import { Users } from '@/features/users'
import type { User } from '@/features/users/types'
import { api } from '@/lib/api'

const users: User[] = [
  {
    id: 41,
    username: 'first-user',
    display_name: '',
    quota: 0,
    used_quota: 0,
    request_count: 0,
    group: 'default',
    status: 1,
    role: 1,
  },
  {
    id: 82,
    username: 'recharge-target',
    display_name: '',
    quota: 500000,
    used_quota: 0,
    request_count: 0,
    group: 'default',
    status: 1,
    role: 1,
  },
]

let queryClient: QueryClient

function renderUsersPage() {
  const rootRoute = createRootRoute()
  const authenticatedRoute = createRoute({
    getParentRoute: () => rootRoute,
    id: '_authenticated',
  })
  const usersRoute = createRoute({
    getParentRoute: () => authenticatedRoute,
    path: 'users',
  })
  const usersIndexRoute = createRoute({
    getParentRoute: () => usersRoute,
    path: '/',
    component: Users,
  })
  const router = createRouter({
    routeTree: rootRoute.addChildren([
      authenticatedRoute.addChildren([
        usersRoute.addChildren([usersIndexRoute]),
      ]),
    ]),
    history: createMemoryHistory({ initialEntries: ['/users/'] }),
  })

  return render(
    <QueryClientProvider client={queryClient}>
      <TooltipProvider>
        <RouterProvider router={router} />
      </TooltipProvider>
    </QueryClientProvider>
  )
}

beforeEach(() => {
  localStorage.clear()
  vi.spyOn(window, 'scrollTo').mockImplementation(() => undefined)
  queryClient = new QueryClient({
    defaultOptions: { queries: { retry: false, gcTime: 0 } },
  })
  vi.spyOn(api, 'get').mockImplementation(async (url, config) => {
    if (url === '/api/user/') {
      return {
        data: {
          success: true,
          data: { items: users, total: 2, page: 1, page_size: 20 },
        },
      }
    }
    if (url === '/api/group/') {
      return { data: { success: true, data: ['default'] } }
    }
    if (url === '/api/authz/catalog') {
      return {
        data: { success: true, data: { resources: [], roles: [] } },
      }
    }
    if (url === '/api/user/82/quota/log') {
      const page = Number(config?.params?.p)
      return {
        data: {
          success: true,
          data: {
            items: [
              {
                id: page,
                user_id: 82,
                created_at: 1700000000,
                source: 'topup',
                reference: page === 1 ? 'Latest recharge' : 'Earlier recharge',
                delta: 500000,
                request_id: '',
                operator_id: 0,
              },
            ],
            total: 11,
            page,
            page_size: Number(config?.params?.page_size),
          },
        },
      }
    }
    throw new Error(`Unexpected GET ${url}`)
  })
})

afterEach(() => {
  cleanup()
  queryClient.clear()
  localStorage.clear()
  vi.restoreAllMocks()
})

describe('User quota history menu', () => {
  it('loads quota records only after opening the selected user quota history', async () => {
    const user = userEvent.setup()
    renderUsersPage()

    const targetRow = await screen.findByRole('row', {
      name: /recharge-target/,
    })
    expect(screen.queryByRole('dialog')).not.toBeInTheDocument()
    expect(api.get).not.toHaveBeenCalledWith(
      expect.stringMatching(/\/quota\/log$/),
      expect.anything()
    )

    await user.click(
      within(targetRow).getByRole('button', { name: 'Open menu' })
    )
    expect(api.get).not.toHaveBeenCalledWith(
      expect.stringMatching(/\/quota\/log$/),
      expect.anything()
    )
    await user.click(
      await screen.findByRole('menuitem', { name: 'Quota History' })
    )

    const dialog = await screen.findByRole('dialog', { name: 'Quota History' })
    expect(dialog).toHaveAccessibleDescription('recharge-target (User ID: 82)')
    expect(
      await within(dialog).findByText('Latest recharge')
    ).toBeInTheDocument()
    expect(api.get).toHaveBeenCalledWith(
      '/api/user/82/quota/log',
      expect.objectContaining({
        params: { p: 1, page_size: 10, view: 'credits' },
      })
    )
    expect(api.get).not.toHaveBeenCalledWith(
      '/api/user/41/quota/log',
      expect.anything()
    )
  })

  it('returns to the first page when the user closes and reopens quota history', async () => {
    const user = userEvent.setup()
    renderUsersPage()
    const targetRow = await screen.findByRole('row', {
      name: /recharge-target/,
    })
    await user.click(
      within(targetRow).getByRole('button', { name: 'Open menu' })
    )
    await user.click(
      await screen.findByRole('menuitem', { name: 'Quota History' })
    )
    const dialog = await screen.findByRole('dialog', { name: 'Quota History' })
    await within(dialog).findByText('Latest recharge')

    await user.click(
      within(dialog).getByRole('button', { name: 'Go to next page' })
    )
    expect(
      await within(dialog).findByText('Earlier recharge')
    ).toBeInTheDocument()
    expect(within(dialog).getByText('Page 2 of 2')).toBeInTheDocument()
    expect(api.get).toHaveBeenCalledWith(
      '/api/user/82/quota/log',
      expect.objectContaining({
        params: { p: 2, page_size: 10, view: 'credits' },
      })
    )
    await user.click(within(dialog).getByRole('button', { name: 'Close' }))
    await waitFor(() => {
      expect(
        screen.queryByRole('dialog', { name: 'Quota History' })
      ).not.toBeInTheDocument()
    })
    vi.mocked(api.get).mockClear()

    await user.click(
      within(targetRow).getByRole('button', { name: 'Open menu' })
    )
    await user.click(
      await screen.findByRole('menuitem', { name: 'Quota History' })
    )
    const reopenedDialog = await screen.findByRole('dialog', {
      name: 'Quota History',
    })
    expect(
      await within(reopenedDialog).findByText('Latest recharge')
    ).toBeInTheDocument()
    expect(within(reopenedDialog).getByText('Page 1 of 2')).toBeInTheDocument()
    expect(
      within(reopenedDialog).getByRole('button', {
        name: 'Go to previous page',
      })
    ).toBeDisabled()
    expect(api.get).toHaveBeenCalledWith(
      '/api/user/82/quota/log',
      expect.objectContaining({
        params: { p: 1, page_size: 10, view: 'credits' },
      })
    )
    expect(api.get).not.toHaveBeenCalledWith(
      '/api/user/82/quota/log',
      expect.objectContaining({
        params: { p: 2, page_size: 10, view: 'credits' },
      })
    )
  })
})
