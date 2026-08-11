import assert from 'node:assert/strict'
import { describe, test } from 'node:test'

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
