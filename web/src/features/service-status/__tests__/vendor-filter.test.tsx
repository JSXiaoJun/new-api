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
import { render, screen } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import type { ReactNode } from 'react'
import { afterEach, describe, expect, test, vi } from 'vitest'

import type { LiveGroupStatus, LiveStatusData } from '../types'

const getLiveServiceStatus = vi.fn()

vi.mock('../api', () => ({ getLiveServiceStatus }))
vi.mock('@/components/layout', () => ({
  PublicLayout: (props: { children: ReactNode }) => <div>{props.children}</div>,
}))

const { ServiceStatus } = await import('../index')

function statusGroup(group: string, vendor: string): LiveGroupStatus {
  return {
    group,
    description: '',
    vendor,
    short_success_rate: 100,
    window_success_rate: 100,
    models: [
      {
        model_name: `${group}-model`,
        short_success_rate: 100,
        window_success_rate: 100,
        minutes: [{ ts: 60, success_rate: 100 }],
      },
    ],
  }
}

function statusData(groups: LiveGroupStatus[]): LiveStatusData {
  return {
    window_start: 0,
    window_end: 120,
    bucket_seconds: 60,
    short_window_minutes: 5,
    groups,
  }
}

const queryClients: QueryClient[] = []

function renderPage(groups: LiveGroupStatus[]) {
  getLiveServiceStatus.mockResolvedValue({
    success: true,
    data: statusData(groups),
  })
  const queryClient = new QueryClient({
    defaultOptions: { queries: { retry: false } },
  })
  queryClients.push(queryClient)
  render(
    <QueryClientProvider client={queryClient}>
      <ServiceStatus />
    </QueryClientProvider>
  )
}

afterEach(() => {
  queryClients.splice(0).forEach((client) => client.clear())
  getLiveServiceStatus.mockReset()
})

describe('service status vendor filter', () => {
  test('opens on OpenAI and shows only its groups', async () => {
    renderPage([
      statusGroup('gpt-pool', 'openai'),
      statusGroup('cc', 'anthropic'),
    ])

    expect(await screen.findByText('gpt-pool-model')).toBeInTheDocument()
    expect(screen.queryByText('cc-model')).not.toBeInTheDocument()
    expect(screen.getByRole('button', { name: /OpenAI/ })).toHaveAttribute(
      'aria-pressed',
      'true'
    )
  })

  test('switching the category shows that category groups', async () => {
    const user = userEvent.setup()
    renderPage([
      statusGroup('gpt-pool', 'openai'),
      statusGroup('cc', 'anthropic'),
    ])
    await screen.findByText('gpt-pool-model')

    await user.click(screen.getByRole('button', { name: /Anthropic/ }))

    expect(screen.getByText('cc-model')).toBeInTheDocument()
    expect(screen.queryByText('gpt-pool-model')).not.toBeInTheDocument()
  })

  test('clicking the selected category again shows every group', async () => {
    const user = userEvent.setup()
    renderPage([
      statusGroup('gpt-pool', 'openai'),
      statusGroup('cc', 'anthropic'),
    ])
    await screen.findByText('gpt-pool-model')

    await user.click(screen.getByRole('button', { name: /OpenAI/ }))

    expect(screen.getByText('gpt-pool-model')).toBeInTheDocument()
    expect(screen.getByText('cc-model')).toBeInTheDocument()
  })

  test('hides the picker and lists every group when no vendor is set', async () => {
    renderPage([statusGroup('default', ''), statusGroup('vip', '')])

    expect(await screen.findByText('default-model')).toBeInTheDocument()
    expect(screen.getByText('vip-model')).toBeInTheDocument()
    expect(
      screen.queryByRole('group', { name: 'Vendor category' })
    ).not.toBeInTheDocument()
  })
})
