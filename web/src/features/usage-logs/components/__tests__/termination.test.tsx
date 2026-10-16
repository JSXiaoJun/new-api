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
import { afterEach, describe, expect, test } from 'vitest'

import type { UsageLog } from '../../data/schema'
import type { LogOtherData } from '../../types'
import { DetailsDialog } from '../dialogs/details-dialog'
import { StreamTpsCell } from '../timing-metrics-cell'

const queryClients: QueryClient[] = []

const clientDisconnect: LogOtherData = {
  termination: {
    ended_by: 'client',
    phase: 'awaiting_response',
    reason: 'canceled',
    request_sent: true,
    elapsed_ms: 22000,
    client_received_data: false,
  },
}

const upstreamDrop: LogOtherData = {
  termination: {
    ended_by: 'upstream',
    phase: 'awaiting_response',
    reason: 'connection_error',
    request_sent: true,
    elapsed_ms: 1200,
    client_received_data: false,
  },
  admin_info: { termination_error: 'Post "https://***.***/***": EOF' },
}

function makeLog(other: LogOtherData): UsageLog {
  return {
    id: 1,
    user_id: 1,
    created_at: 1,
    type: 2,
    content: '',
    username: 'user',
    token_name: 'token',
    model_name: 'gpt-test',
    quota: 1,
    prompt_tokens: 134035,
    completion_tokens: 0,
    use_time: 22,
    is_stream: true,
    channel: 1,
    channel_name: 'channel',
    token_id: 1,
    group: 'default',
    ip: '',
    other: JSON.stringify(other),
    request_id: 'req-1',
    upstream_request_id: '',
  }
}

function renderDetails(other: LogOtherData, isAdmin: boolean): void {
  const queryClient = new QueryClient({
    defaultOptions: { queries: { retry: false } },
  })
  queryClient.setQueryData(['status'], {}, { updatedAt: Date.now() + 60_000 })
  queryClients.push(queryClient)
  render(
    <QueryClientProvider client={queryClient}>
      <DetailsDialog
        log={makeLog(other)}
        isAdmin={isAdmin}
        isRoot={false}
        open
        onOpenChange={() => undefined}
      />
    </QueryClientProvider>
  )
}

afterEach(() => {
  for (const queryClient of queryClients) {
    queryClient.clear()
  }
  queryClients.length = 0
})

describe('usage log connection termination', () => {
  test('details name the client when it left while upstream had not answered', () => {
    renderDetails(clientDisconnect, false)

    expect(screen.getByText('Connection Termination')).toBeInTheDocument()
    expect(screen.getByText('Client disconnected')).toBeInTheDocument()
    expect(
      screen.getByText('Waiting for upstream response')
    ).toBeInTheDocument()
    expect(screen.getByText('Client closed the connection')).toBeInTheDocument()
    expect(
      screen.getByText('Upstream elapsed').nextElementSibling
    ).toHaveTextContent('22.0s')
  })

  test('details show the upstream transport error to admins only', () => {
    renderDetails(upstreamDrop, true)
    expect(screen.getByText('Upstream disconnected')).toBeInTheDocument()
    expect(
      screen.getByText('Post "https://***.***/***": EOF')
    ).toBeInTheDocument()
  })

  test('details hide the upstream transport error from non-admin users', () => {
    renderDetails(upstreamDrop, false)
    expect(screen.getByText('Upstream disconnected')).toBeInTheDocument()
    expect(screen.queryByText('Transport Error')).toBeNull()
  })

  test('details omit the section when the attempt ended normally', () => {
    renderDetails({}, true)
    expect(screen.queryByText('Connection Termination')).toBeNull()
  })

  test('list cell shows who ended the attempt in place of an empty rate', () => {
    render(
      <StreamTpsCell
        isStream
        tokensPerSecond={null}
        termination={clientDisconnect.termination}
      />
    )
    expect(screen.getByText('Client disconnected')).toBeInTheDocument()
    expect(screen.queryByText('—')).toBeNull()
  })

  test('list cell keeps the rate when output was produced', () => {
    render(
      <StreamTpsCell
        isStream
        tokensPerSecond={6}
        termination={clientDisconnect.termination}
      />
    )
    expect(screen.getByText('6 t/s')).toBeInTheDocument()
    expect(screen.queryByText('Client disconnected')).toBeNull()
  })
})
