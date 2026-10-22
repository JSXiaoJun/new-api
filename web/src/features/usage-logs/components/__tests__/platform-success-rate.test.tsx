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
import { afterEach, beforeEach, describe, expect, test, vi } from 'vitest'

import type { UsageLog } from '../../data/schema'
import {
  LogSuccessRatesProvider,
  useLogSuccessRate,
  useLogSuccessRatesQuery,
} from '../log-success-rates'
import { StreamTpsCell } from '../timing-metrics-cell'

const { post } = vi.hoisted(() => ({ post: vi.fn() }))

vi.mock('@/lib/api', () => ({ api: { post } }))

const queryClients: QueryClient[] = []

function makeLog(id: number, type: number, endedBy?: string): UsageLog {
  return {
    id,
    user_id: 1,
    created_at: Math.floor(Date.now() / 1000) - 60,
    type,
    content: '',
    username: 'user',
    token_name: 'token',
    model_name: 'gpt-test',
    quota: 0,
    prompt_tokens: 0,
    completion_tokens: 0,
    use_time: 3,
    is_stream: true,
    channel: 1,
    channel_name: 'channel',
    token_id: 1,
    group: 'pro',
    ip: '',
    other: endedBy
      ? JSON.stringify({ termination: { ended_by: endedBy } })
      : '',
    request_id: '',
    upstream_request_id: '',
  }
}

function RateProbe(props: { logId: number }) {
  const rate = useLogSuccessRate(props.logId)
  return (
    <span data-testid={`rate-${props.logId}`}>
      {rate ? `${rate.rate} in ${rate.windowMinutes}m` : 'none'}
    </span>
  )
}

function RatesTable(props: { logs: UsageLog[] }) {
  const rates = useLogSuccessRatesQuery(props.logs, true)
  return (
    <LogSuccessRatesProvider rates={rates}>
      {props.logs.map((log) => (
        <RateProbe key={log.id} logId={log.id} />
      ))}
    </LogSuccessRatesProvider>
  )
}

function renderWithQueryClient(node: React.ReactNode): void {
  const queryClient = new QueryClient({
    defaultOptions: { queries: { retry: false } },
  })
  queryClients.push(queryClient)
  render(<QueryClientProvider client={queryClient}>{node}</QueryClientProvider>)
}

beforeEach(() => {
  post.mockReset()
})

afterEach(() => {
  for (const queryClient of queryClients) queryClient.clear()
  queryClients.length = 0
})

describe('platform success rate beside failed logs', () => {
  test('requests rates for failed logs only and assigns them by log id', async () => {
    post.mockResolvedValue({
      data: {
        success: true,
        data: { window_minutes: 5, success_rates: [97.5, null] },
      },
    })
    const logs = [makeLog(1, 2, 'upstream'), makeLog(2, 2), makeLog(3, 5)]

    renderWithQueryClient(<RatesTable logs={logs} />)

    await waitFor(() => {
      expect(screen.getByTestId('rate-1')).toHaveTextContent('97.5 in 5m')
    })
    expect(post).toHaveBeenCalledTimes(1)
    expect(post.mock.calls[0][0]).toBe('/api/log/success_rate')
    expect(post.mock.calls[0][1].points).toHaveLength(2)
    expect(screen.getByTestId('rate-2')).toHaveTextContent('none')
    expect(screen.getByTestId('rate-3')).toHaveTextContent('none')
  })

  test('sends no request when the page has no failed logs', () => {
    renderWithQueryClient(<RatesTable logs={[makeLog(1, 2)]} />)

    expect(post).not.toHaveBeenCalled()
    expect(screen.getByTestId('rate-1')).toHaveTextContent('none')
  })

  test('list cell shows the platform success rate and its window under the disconnect', () => {
    render(
      <StreamTpsCell
        isStream
        tokensPerSecond={null}
        termination={{ ended_by: 'upstream' }}
        platformSuccessRate={{ rate: 98.5, windowMinutes: 5 }}
      />
    )

    expect(screen.getByText('Upstream disconnected')).toBeInTheDocument()
    expect(
      screen.getByText('Platform success rate within 5 minutes')
    ).toBeInTheDocument()
    expect(screen.getByText('98.50%')).toBeInTheDocument()
  })

  test('list cell shows no platform success rate when none is known', () => {
    render(
      <StreamTpsCell
        isStream
        tokensPerSecond={null}
        termination={{ ended_by: 'upstream' }}
        platformSuccessRate={null}
      />
    )

    expect(screen.queryByText(/Platform success rate/)).toBeNull()
  })
})
