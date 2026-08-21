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
import { assert, describe, test } from 'vitest'

import {
  buildDefaultDiscountWindow,
  getDiscountGroupOptions,
  parseDiscountSchedule,
} from '../discount-schedule'

describe('discount schedule helpers', () => {
  test('parses saved schedules and preserves selected groups', () => {
    const schedule = parseDiscountSchedule(
      '{"enabled":true,"ratio":0.8,"groups":["vip","default"],"daily_repeat":true,"start_time":"22:00","end_time":"06:00","timezone":"Asia/Shanghai"}'
    )

    assert.equal(schedule.enabled, true)
    assert.equal(schedule.ratio, 0.8)
    assert.deepEqual(schedule.groups, ['vip', 'default'])
    assert.equal(schedule.daily_repeat, true)
  })

  test('merges pricing and selectable group sources without duplicates', () => {
    const options = getDiscountGroupOptions(
      '{"default":1,"vip":1.5}',
      '{"default":"Default group","svip":"SVIP group"}'
    )

    assert.deepEqual(options, [
      { value: 'default', label: 'default - Default group' },
      { value: 'svip', label: 'svip - SVIP group' },
      { value: 'vip', label: 'vip' },
    ])
  })

  test('creates a one-hour default window for an unsaved schedule', () => {
    const window = buildDefaultDiscountWindow(new Date(2026, 7, 8, 12, 30, 45))

    assert.equal(window.startAt, '2026-08-08T12:30')
    assert.equal(window.endAt, '2026-08-08T13:30')
  })
})
