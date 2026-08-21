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
  buildDefaultScheduleDate,
  createTrafficControlSchema,
  parseTrafficControlSchedule,
} from '../traffic-control-schedule'

const translate = (key: string) => key

describe('traffic control schedule', () => {
  test('preserves multiple daily time periods from saved JSON', () => {
    const schedule = parseTrafficControlSchedule(
      '{"enabled":true,"daily_repeat":true,"date":"","time_ranges":[{"start_time":"08:00","end_time":"10:00"},{"start_time":"22:00","end_time":"06:00"}],"timezone":"Asia/Shanghai"}'
    )

    assert.equal(schedule.enabled, true)
    assert.equal(schedule.daily_repeat, true)
    assert.deepEqual(schedule.time_ranges, [
      { start_time: '08:00', end_time: '10:00' },
      { start_time: '22:00', end_time: '06:00' },
    ])
  })

  test('requires a selected date for enabled one-time schedules', () => {
    const result = createTrafficControlSchema(translate).safeParse({
      traffic_control: {
        mainland_web_block_enabled: false,
        include_hong_kong_taiwan: false,
        country_header: 'CF-IPCountry',
        warning_title: '',
        warning_content: '',
        warning_annotation: '',
      },
      schedule: {
        enabled: true,
        daily_repeat: false,
        date: '',
        time_ranges: [{ start_time: '09:00', end_time: '18:00' }],
        timezone: 'Asia/Shanghai',
      },
    })

    assert.equal(result.success, false)
    if (!result.success) {
      assert.ok(
        result.error.issues.some(
          (issue) => issue.message === 'Schedule date is required'
        )
      )
    }
  })

  test('formats the default one-time date in local calendar time', () => {
    assert.equal(
      buildDefaultScheduleDate(new Date(2026, 7, 11, 23, 30)),
      '2026-08-11'
    )
  })
})
