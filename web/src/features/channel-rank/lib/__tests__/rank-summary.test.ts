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
import { describe, expect, test } from 'vitest'

import type { ChannelQuotaRank } from '../../types'
import {
  getDayRange,
  getNetQuotaShare,
  summarizeChannelRank,
} from '../rank-summary'

function rank(overrides: Partial<ChannelQuotaRank>): ChannelQuotaRank {
  return {
    channel_id: 1,
    channel_name: '',
    request_count: 0,
    prompt_tokens: 0,
    completion_tokens: 0,
    consumed_quota: 0,
    refunded_quota: 0,
    net_quota: 0,
    ...overrides,
  }
}

describe('channel rank summary', () => {
  test('sums net, consumed, refunded quota and requests across channels', () => {
    const summary = summarizeChannelRank([
      rank({
        channel_id: 2,
        request_count: 1,
        consumed_quota: 200,
        net_quota: 200,
      }),
      rank({
        channel_id: 1,
        request_count: 2,
        consumed_quota: 150,
        refunded_quota: 30,
        net_quota: 120,
      }),
    ])

    expect(summary).toEqual({
      netQuota: 320,
      consumedQuota: 350,
      refundedQuota: 30,
      requestCount: 3,
      channelCount: 2,
    })
  })

  test('returns zero totals for a day without consumption', () => {
    expect(summarizeChannelRank([])).toEqual({
      netQuota: 0,
      consumedQuota: 0,
      refundedQuota: 0,
      requestCount: 0,
      channelCount: 0,
    })
  })
})

describe('channel net quota share', () => {
  test('returns the percentage of the day total', () => {
    expect(getNetQuotaShare(80, 320)).toBe(25)
  })

  test('returns null when the day total is not positive', () => {
    expect(getNetQuotaShare(0, 0)).toBeNull()
    expect(getNetQuotaShare(-10, -10)).toBeNull()
  })
})

describe('channel rank day range', () => {
  test('covers exactly one local calendar day from midnight', () => {
    const range = getDayRange(new Date(2026, 9, 22, 15, 30, 0))

    expect(range.start).toBe(new Date(2026, 9, 22, 0, 0, 0).getTime() / 1000)
    expect(range.end).toBe(new Date(2026, 9, 23, 0, 0, 0).getTime() / 1000)
  })
})
