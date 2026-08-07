import { z } from 'zod'

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
import { safeJsonParse } from '../utils/json-parser'

type Translate = (key: string) => string

export const createDiscountSettingsSchema = (t: Translate) =>
  z
    .object({
      enabled: z.boolean(),
      ratio: z.coerce
        .number()
        .finite()
        .min(0.01, t('Discount multiplier cannot be less than 0.01'))
        .max(1, t('Discount multiplier cannot be greater than 1')),
      groups: z.array(z.string()),
      daily_repeat: z.boolean(),
      start_at: z.string(),
      end_at: z.string(),
      start_time: z.string(),
      end_time: z.string(),
      timezone: z.string().trim().min(1, t('Timezone is required')),
    })
    .superRefine((values, context) => {
      if (!values.enabled) return
      if (values.groups.length === 0) {
        context.addIssue({
          code: 'custom',
          path: ['groups'],
          message: t('Select at least one discounted user group'),
        })
      }
      if (values.daily_repeat) {
        if (!/^(?:[01]\d|2[0-3]):[0-5]\d$/.test(values.start_time)) {
          context.addIssue({
            code: 'custom',
            path: ['start_time'],
            message: t('Start time is required'),
          })
        }
        if (!/^(?:[01]\d|2[0-3]):[0-5]\d$/.test(values.end_time)) {
          context.addIssue({
            code: 'custom',
            path: ['end_time'],
            message: t('End time is required'),
          })
        }
        if (values.start_time === values.end_time) {
          context.addIssue({
            code: 'custom',
            path: ['end_time'],
            message: t('Start and end times must be different'),
          })
        }
        return
      }
      if (!values.start_at) {
        context.addIssue({
          code: 'custom',
          path: ['start_at'],
          message: t('Start date and time are required'),
        })
      }
      if (!values.end_at || values.end_at <= values.start_at) {
        context.addIssue({
          code: 'custom',
          path: ['end_at'],
          message: t('End date and time must be after the start'),
        })
      }
    })

export type DiscountSettingsValues = z.infer<
  ReturnType<typeof createDiscountSettingsSchema>
>

export type DiscountSchedule = {
  enabled: boolean
  ratio: number
  groups: string[]
  daily_repeat: boolean
  start_at: string
  end_at: string
  start_time: string
  end_time: string
  timezone: string
}

export const DEFAULT_DISCOUNT_SCHEDULE: DiscountSchedule = {
  enabled: false,
  ratio: 1,
  groups: [],
  daily_repeat: false,
  start_at: '',
  end_at: '',
  start_time: '22:00',
  end_time: '06:00',
  timezone: 'Asia/Shanghai',
}

export function parseDiscountSchedule(value: string): DiscountSchedule {
  const parsed = safeJsonParse<Partial<DiscountSchedule>>(value, {
    fallback: {},
    silent: true,
  })

  return {
    enabled: parsed.enabled === true,
    ratio:
      typeof parsed.ratio === 'number' && Number.isFinite(parsed.ratio)
        ? parsed.ratio
        : DEFAULT_DISCOUNT_SCHEDULE.ratio,
    groups: Array.isArray(parsed.groups)
      ? parsed.groups.filter(
          (group): group is string => typeof group === 'string'
        )
      : [],
    daily_repeat: parsed.daily_repeat === true,
    start_at: typeof parsed.start_at === 'string' ? parsed.start_at : '',
    end_at: typeof parsed.end_at === 'string' ? parsed.end_at : '',
    start_time:
      typeof parsed.start_time === 'string'
        ? parsed.start_time
        : DEFAULT_DISCOUNT_SCHEDULE.start_time,
    end_time:
      typeof parsed.end_time === 'string'
        ? parsed.end_time
        : DEFAULT_DISCOUNT_SCHEDULE.end_time,
    timezone:
      typeof parsed.timezone === 'string' && parsed.timezone.trim() !== ''
        ? parsed.timezone
        : DEFAULT_DISCOUNT_SCHEDULE.timezone,
  }
}

export function getDiscountGroupOptions(
  groupRatioValue: string,
  usableGroupsValue: string
): Array<{ label: string; value: string }> {
  const ratioMap = safeJsonParse<Record<string, number>>(groupRatioValue, {
    fallback: {},
    silent: true,
  })
  const usableMap = safeJsonParse<Record<string, string>>(usableGroupsValue, {
    fallback: {},
    silent: true,
  })
  const names = [
    ...new Set([...Object.keys(ratioMap), ...Object.keys(usableMap)]),
  ]
  names.sort((a, b) => a.localeCompare(b))
  return names.map((name) => ({
    value: name,
    label: usableMap[name] ? `${name} - ${usableMap[name]}` : name,
  }))
}

export function buildDefaultDiscountWindow(now: Date): {
  startAt: string
  endAt: string
} {
  const format = (date: Date) => {
    const year = date.getFullYear()
    const month = String(date.getMonth() + 1).padStart(2, '0')
    const day = String(date.getDate()).padStart(2, '0')
    const hour = String(date.getHours()).padStart(2, '0')
    const minute = String(date.getMinutes()).padStart(2, '0')
    return `${year}-${month}-${day}T${hour}:${minute}`
  }

  const start = new Date(now)
  start.setSeconds(0, 0)
  const end = new Date(start.getTime() + 60 * 60 * 1000)
  return { startAt: format(start), endAt: format(end) }
}
