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

import type { UsageLog } from '../../data/schema'
import type { LogOtherData } from '../../types'
import {
  collectSuccessRatePoints,
  isPlatformFailureLog,
  SUCCESS_RATE_RETENTION_SECONDS,
} from '../success-rate'

const NOW = 1_800_000_000

function makeLog(
  id: number,
  type: number,
  other: LogOtherData = {},
  createdAt = NOW - 60
): UsageLog {
  return {
    id,
    user_id: 1,
    created_at: createdAt,
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
    other: JSON.stringify(other),
    request_id: '',
    upstream_request_id: '',
  }
}

function terminated(endedBy: string): LogOtherData {
  return { termination: { ended_by: endedBy, phase: 'receiving' } }
}

describe('platform failure logs', () => {
  test.each([
    ['error log', makeLog(1, 5), true],
    ['upstream disconnect', makeLog(1, 2, terminated('upstream')), true],
    ['gateway interruption', makeLog(1, 2, terminated('gateway')), true],
    ['client disconnect', makeLog(1, 2, terminated('client')), false],
    [
      'safety policy block',
      makeLog(1, 2, {
        ...terminated('upstream'),
        policy_block: 'cyber_policy',
      }),
      false,
    ],
    ['completed request', makeLog(1, 2), false],
    ['top-up log', makeLog(1, 1), false],
  ])('%s is a platform failure: %s', (_name, log, expected) => {
    expect(isPlatformFailureLog(log)).toBe(expected)
  })
})

describe('success rate points', () => {
  test('collects recent failed logs in order with their ids', () => {
    const logs = [
      makeLog(10, 5),
      makeLog(11, 2),
      makeLog(12, 2, terminated('upstream'), NOW - 120),
    ]

    expect(collectSuccessRatePoints(logs, NOW)).toEqual({
      ids: [10, 12],
      points: [
        { created_at: NOW - 60, group: 'pro', model_name: 'gpt-test' },
        { created_at: NOW - 120, group: 'pro', model_name: 'gpt-test' },
      ],
    })
  })

  test('auto-group error log looks up the group its attempt routed to', () => {
    const autoLog = { ...makeLog(30, 5, { auto_group: 'vip' }), group: 'auto' }

    expect(collectSuccessRatePoints([autoLog], NOW).points).toEqual([
      { created_at: NOW - 60, group: 'vip', model_name: 'gpt-test' },
    ])
  })

  test('skips failed logs older than the retention or without a group', () => {
    const old = makeLog(20, 5, {}, NOW - SUCCESS_RATE_RETENTION_SECONDS - 1)
    const noGroup = { ...makeLog(21, 5), group: '' }

    expect(collectSuccessRatePoints([old, noGroup], NOW)).toEqual({
      ids: [],
      points: [],
    })
  })
})
