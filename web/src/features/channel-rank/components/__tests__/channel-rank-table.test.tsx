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
import { render, screen, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import type { ReactElement, ReactNode } from 'react'
import { afterEach, describe, expect, test, vi } from 'vitest'

import { getChannelUserQuotaRank } from '../../api'
import type { ChannelQuotaRank, ChannelUserQuotaRank } from '../../types'
import { ChannelRankTable } from '../channel-rank-table'

vi.mock('../../api', () => ({
  getChannelUserQuotaRank: vi.fn(),
}))

vi.mock('@tanstack/react-router', () => ({
  Link: (props: {
    search: { channel: string; startTime: number; endTime: number }
    children?: ReactNode
  }) => (
    <a
      href={`/usage-logs/common?channel=${props.search.channel}&startTime=${props.search.startTime}&endTime=${props.search.endTime}`}
    >
      {props.children}
    </a>
  ),
}))

const range = { start: 1_792_540_800, end: 1_792_627_200 }

function renderWithQuery(ui: ReactElement) {
  const queryClient = new QueryClient({
    defaultOptions: { queries: { retry: false } },
  })
  return render(
    <QueryClientProvider client={queryClient}>{ui}</QueryClientProvider>
  )
}

function userRank(
  overrides: Partial<ChannelUserQuotaRank>
): ChannelUserQuotaRank {
  return {
    user_id: 1,
    username: '',
    request_count: 1,
    consumed_quota: 0,
    refunded_quota: 0,
    net_quota: 0,
    ...overrides,
  }
}

afterEach(() => {
  vi.mocked(getChannelUserQuotaRank).mockReset()
})

function rank(overrides: Partial<ChannelQuotaRank>): ChannelQuotaRank {
  return {
    channel_id: 1,
    channel_name: '',
    request_count: 1,
    prompt_tokens: 0,
    completion_tokens: 0,
    consumed_quota: 0,
    refunded_quota: 0,
    net_quota: 0,
    ...overrides,
  }
}

describe('channel rank table', () => {
  test('keeps server order and labels deleted and channel-less rows', () => {
    renderWithQuery(
      <ChannelRankTable
        rows={[
          rank({ channel_id: 2, channel_name: 'west', net_quota: 200 }),
          rank({ channel_id: 9, net_quota: 150 }),
          rank({ channel_id: 0, net_quota: 50 }),
        ]}
        loading={false}
        totalNetQuota={400}
        range={range}
      />
    )

    const bodyRows = screen.getAllByRole('row').slice(1)
    expect(bodyRows).toHaveLength(3)
    expect(within(bodyRows[0]).getByText('west')).toBeInTheDocument()
    expect(within(bodyRows[0]).getByText('50%')).toBeInTheDocument()
    expect(within(bodyRows[1]).getByText('Deleted channel')).toBeInTheDocument()
    expect(within(bodyRows[2]).getByText('No channel')).toBeInTheDocument()
  })

  test('opening view share loads that channel top users with their share of the channel', async () => {
    vi.mocked(getChannelUserQuotaRank).mockResolvedValue([
      userRank({ user_id: 7, username: 'bob', net_quota: 150 }),
      userRank({ user_id: 3, net_quota: 50 }),
    ])
    renderWithQuery(
      <ChannelRankTable
        rows={[rank({ channel_id: 2, channel_name: 'west', net_quota: 200 })]}
        loading={false}
        totalNetQuota={400}
        range={range}
      />
    )

    expect(getChannelUserQuotaRank).not.toHaveBeenCalled()
    await userEvent.click(screen.getByRole('button', { name: 'View share' }))

    const dialog = await screen.findByRole('dialog')
    expect(getChannelUserQuotaRank).toHaveBeenCalledWith(
      2,
      range.start,
      range.end
    )
    const userRows = (await within(dialog).findAllByRole('row')).slice(1)
    expect(userRows).toHaveLength(2)
    expect(within(userRows[0]).getByText('bob')).toBeInTheDocument()
    expect(within(userRows[0]).getByText('75%')).toBeInTheDocument()
    expect(within(userRows[1]).getByText('Deleted user')).toBeInTheDocument()
    expect(within(userRows[1]).getByText('25%')).toBeInTheDocument()
    expect(
      within(dialog).getByRole('link', { name: 'View logs' })
    ).toHaveAttribute(
      'href',
      `/usage-logs/common?channel=2&startTime=${range.start * 1000}&endTime=${range.end * 1000 - 1000}`
    )
  })

  test('view share shows the empty state when the channel has no user consumption', async () => {
    vi.mocked(getChannelUserQuotaRank).mockResolvedValue([])
    renderWithQuery(
      <ChannelRankTable
        rows={[rank({ channel_id: 2, channel_name: 'west', net_quota: 0 })]}
        loading={false}
        totalNetQuota={0}
        range={range}
      />
    )

    await userEvent.click(screen.getByRole('button', { name: 'View share' }))

    expect(
      await screen.findByText('No user consumption on this channel')
    ).toBeInTheDocument()
  })

  test('shows the empty state when the day has no consumption', () => {
    renderWithQuery(
      <ChannelRankTable
        rows={[]}
        loading={false}
        totalNetQuota={0}
        range={range}
      />
    )

    expect(
      screen.getByText('No channel consumption on this day')
    ).toBeInTheDocument()
  })
})
