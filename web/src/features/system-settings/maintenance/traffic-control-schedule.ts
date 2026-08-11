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
import * as z from 'zod'

import { safeJsonParse } from '../utils/json-parser'

type Translate = (key: string) => string

const timePattern = /^(?:[01]\d|2[0-3]):[0-5]\d$/

export const createTrafficControlSchema = (t: Translate) =>
  z
    .object({
      traffic_control: z.object({
        mainland_web_block_enabled: z.boolean(),
        include_hong_kong_taiwan: z.boolean(),
        country_header: z.string().trim().min(1),
        warning_title: z.string(),
        warning_content: z.string(),
        warning_annotation: z.string(),
      }),
      schedule: z.object({
        enabled: z.boolean(),
        daily_repeat: z.boolean(),
        date: z.string(),
        time_ranges: z
          .array(
            z.object({
              start_time: z
                .string()
                .regex(timePattern, t('Start time is required')),
              end_time: z
                .string()
                .regex(timePattern, t('End time is required')),
            })
          )
          .max(24),
        timezone: z.string().trim().min(1, t('Timezone is required')),
      }),
    })
    .superRefine((values, context) => {
      if (!values.schedule.enabled) return
      if (values.schedule.time_ranges.length === 0) {
        context.addIssue({
          code: 'custom',
          path: ['schedule', 'time_ranges'],
          message: t('Add at least one time period'),
        })
      }
      if (!values.schedule.daily_repeat && !values.schedule.date) {
        context.addIssue({
          code: 'custom',
          path: ['schedule', 'date'],
          message: t('Schedule date is required'),
        })
      }
      values.schedule.time_ranges.forEach((timeRange, index) => {
        if (timeRange.start_time === timeRange.end_time) {
          context.addIssue({
            code: 'custom',
            path: ['schedule', 'time_ranges', index, 'end_time'],
            message: t('Start and end times must be different'),
          })
        }
      })
    })

export type TrafficControlFormValues = z.infer<
  ReturnType<typeof createTrafficControlSchema>
>

export type TrafficControlSchedule = TrafficControlFormValues['schedule']

export const DEFAULT_TRAFFIC_CONTROL_SCHEDULE: TrafficControlSchedule = {
  enabled: false,
  daily_repeat: true,
  date: '',
  time_ranges: [{ start_time: '09:00', end_time: '18:00' }],
  timezone: 'Asia/Shanghai',
}

export const DEFAULT_TRAFFIC_CONTROL_SCHEDULE_JSON = JSON.stringify(
  DEFAULT_TRAFFIC_CONTROL_SCHEDULE
)

export function parseTrafficControlSchedule(
  value: string
): TrafficControlSchedule {
  const parsed = safeJsonParse<Partial<TrafficControlSchedule>>(value, {
    fallback: {},
    silent: true,
  })
  const timeRanges = Array.isArray(parsed.time_ranges)
    ? parsed.time_ranges.filter(
        (timeRange): timeRange is { start_time: string; end_time: string } =>
          typeof timeRange === 'object' &&
          timeRange !== null &&
          typeof timeRange.start_time === 'string' &&
          typeof timeRange.end_time === 'string'
      )
    : DEFAULT_TRAFFIC_CONTROL_SCHEDULE.time_ranges

  return {
    enabled: parsed.enabled === true,
    daily_repeat: parsed.daily_repeat !== false,
    date: typeof parsed.date === 'string' ? parsed.date : '',
    time_ranges: timeRanges,
    timezone:
      typeof parsed.timezone === 'string' && parsed.timezone.trim() !== ''
        ? parsed.timezone
        : DEFAULT_TRAFFIC_CONTROL_SCHEDULE.timezone,
  }
}

export function buildDefaultScheduleDate(now: Date): string {
  const year = now.getFullYear()
  const month = String(now.getMonth() + 1).padStart(2, '0')
  const day = String(now.getDate()).padStart(2, '0')
  return `${year}-${month}-${day}`
}
