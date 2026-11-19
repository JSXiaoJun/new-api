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
  createRouter,
  RouterProvider,
} from '@tanstack/react-router'
import { cleanup, render, screen } from '@testing-library/react'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'

import { api } from '@/lib/api'
import { ROLE } from '@/lib/roles'
import { useAuthStore } from '@/stores/auth-store'
import { useSystemConfigStore } from '@/stores/system-config-store'

import { OverviewDashboard } from '../overview-dashboard'

vi.mock('../api-info-panel', () => ({
  ApiInfoPanel: () => <section aria-label='API info panel' />,
}))
vi.mock('../announcements-panel', () => ({
  AnnouncementsPanel: () => <section aria-label='Announcements panel' />,
}))
vi.mock('../faq-panel', () => ({
  FAQPanel: () => <section aria-label='FAQ panel' />,
}))
vi.mock('../uptime-panel', () => ({
  UptimePanel: () => <section aria-label='Uptime panel' />,
}))
vi.mock('../performance-health-panel', () => ({
  PerformanceHealthPanel: () => (
    <section aria-label='Performance health panel' />
  ),
}))

let client: QueryClient

beforeEach(() => {
  window.localStorage.clear()
  window.localStorage.setItem(
    'dashboard_overview_setup_guide_expanded',
    'collapsed'
  )
  useSystemConfigStore.setState(useSystemConfigStore.getInitialState(), true)
  client = new QueryClient({
    defaultOptions: { queries: { retry: false } },
  })
  vi.spyOn(api, 'get').mockImplementation(async (url) => {
    switch (url) {
      case '/api/token/?p=1&size=10':
        return { data: { success: true, data: { items: [] } } }
      case '/api/status':
        return {
          data: {
            data: {
              api_info_enabled: true,
              announcements_enabled: true,
              faq_enabled: true,
              uptime_kuma_enabled: true,
            },
          },
        }
      case '/api/user/models':
        return { data: { success: true, data: [] } }
      case '/api/data/self':
        return { data: { success: true, data: [] } }
      default:
        return { data: { success: true, data: [] } }
    }
  })
})

afterEach(() => {
  cleanup()
  client.clear()
  useAuthStore.setState(useAuthStore.getInitialState(), true)
  useSystemConfigStore.setState(useSystemConfigStore.getInitialState(), true)
  window.localStorage.clear()
  vi.restoreAllMocks()
})

async function renderOverview(role: number) {
  useAuthStore.getState().auth.setUser({
    id: 1,
    username: 'dashboard-user',
    role,
    quota: 1000000,
  })
  const router = createRouter({
    routeTree: createRootRoute({ component: OverviewDashboard }),
    history: createMemoryHistory({ initialEntries: ['/'] }),
  })
  await router.load()
  return render(
    <QueryClientProvider client={client}>
      <RouterProvider router={router} />
    </QueryClientProvider>
  )
}

function isBefore(first: HTMLElement, second: HTMLElement): boolean {
  return Boolean(
    first.compareDocumentPosition(second) & Node.DOCUMENT_POSITION_FOLLOWING
  )
}

describe('overview layout', () => {
  it('places API info, announcements and FAQ before performance health for admins', async () => {
    await renderOverview(ROLE.ADMIN)

    const apiInfo = await screen.findByRole('region', {
      name: 'API info panel',
    })
    const announcements = screen.getByRole('region', {
      name: 'Announcements panel',
    })
    const faq = screen.getByRole('region', { name: 'FAQ panel' })
    const performance = screen.getByRole('region', {
      name: 'Performance health panel',
    })

    expect(isBefore(apiInfo, announcements)).toBe(true)
    expect(isBefore(announcements, faq)).toBe(true)
    expect(isBefore(faq, performance)).toBe(true)
    expect(
      isBefore(
        screen.getByRole('region', { name: 'Uptime panel' }),
        performance
      )
    ).toBe(true)
  })

  it('shows the balance card without the usage overview block', async () => {
    await renderOverview(ROLE.USER)

    expect(await screen.findByText('Credit remaining')).toBeVisible()
    expect(screen.getByText('Wallet').closest('a')).toHaveAttribute(
      'href',
      '/wallet'
    )
    expect(screen.queryByText('Usage at a glance')).not.toBeInTheDocument()
    expect(screen.queryByText('Historical Usage')).not.toBeInTheDocument()
    expect(
      screen.queryByRole('region', { name: 'Performance health panel' })
    ).not.toBeInTheDocument()
  })
})
