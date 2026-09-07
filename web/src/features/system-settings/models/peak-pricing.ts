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
import { z } from 'zod'

const price = z.number().finite().nonnegative().optional()
export const peakTariffSchema = z
  .object({
    mode: z.enum(['per_token', 'per_request', 'per_second', 'tiered_expr']),
    price,
    input_price: price,
    output_price: price,
    cache_price: price,
    create_cache_price: price,
    image_price: price,
    audio_price: price,
    audio_output_price: price,
    expression: z.string().optional(),
  })
  .superRefine((tariff, ctx) => {
    if (tariff.mode === 'tiered_expr') {
      if (!tariff.expression?.trim()) {
        ctx.addIssue({
          code: 'custom',
          message: 'Billing expression is required.',
        })
      }
      return
    }
    if (tariff.mode !== 'per_token') {
      if (tariff.price === undefined) {
        ctx.addIssue({ code: 'custom', message: 'Price is required.' })
      }
      return
    }
    if (tariff.input_price === undefined || tariff.output_price === undefined) {
      ctx.addIssue({
        code: 'custom',
        message: 'Input and output prices are required.',
      })
    }
    if (
      tariff.input_price === 0 &&
      [
        tariff.output_price,
        tariff.cache_price,
        tariff.create_cache_price,
        tariff.image_price,
        tariff.audio_price,
        tariff.audio_output_price,
      ].some((value) => (value ?? 0) > 0)
    ) {
      ctx.addIssue({
        code: 'custom',
        message:
          'Use expression pricing when input is free and other tokens are paid.',
      })
    }
    if (
      (tariff.audio_output_price ?? 0) > 0 &&
      !(tariff.audio_price && tariff.audio_price > 0)
    ) {
      ctx.addIssue({
        code: 'custom',
        message: 'Audio output price requires an audio input price.',
      })
    }
  })

const time = z.string().regex(/^([01]\d|2[0-3]):[0-5]\d$/)
export const peakPricingSchema = z
  .object({
    timezone: z.string().refine((value) => {
      if (!value || value === 'Local') return false
      try {
        new Intl.DateTimeFormat('en', { timeZone: value })
        return true
      } catch {
        return false
      }
    }, 'Invalid timezone.'),
    default: peakTariffSchema,
    periods: z
      .array(z.object({ start: time, end: time, tariff: peakTariffSchema }))
      .min(1)
      .max(48),
  })
  .superRefine((schedule, ctx) => {
    const occupied = new Set<number>()
    for (const period of schedule.periods) {
      const [sh, sm] = period.start.split(':').map(Number)
      const [eh, em] = period.end.split(':').map(Number)
      const start = sh * 60 + sm
      const end = eh * 60 + em
      if (
        !Number.isFinite(start) ||
        !Number.isFinite(end) ||
        start < 0 ||
        end < 0 ||
        start >= 1440 ||
        end >= 1440
      ) {
        return
      }
      if (start === end) {
        ctx.addIssue({
          code: 'custom',
          message: 'Start and end times must differ.',
        })
        return
      }
      for (let minute = start; minute !== end; minute = (minute + 1) % 1440) {
        if (occupied.has(minute)) {
          ctx.addIssue({
            code: 'custom',
            message: 'Time periods must not overlap.',
          })
          return
        }
        occupied.add(minute)
      }
    }
  })

export type PeakTariff = z.infer<typeof peakTariffSchema>
export type PeakPricing = z.infer<typeof peakPricingSchema>

export function createPeakPricing(): PeakPricing {
  return {
    timezone: 'Asia/Shanghai',
    default: { mode: 'per_request' },
    periods: [
      { start: '09:00', end: '18:00', tariff: { mode: 'per_request' } },
    ],
  }
}
