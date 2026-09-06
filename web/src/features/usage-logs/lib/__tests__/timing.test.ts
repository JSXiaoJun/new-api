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

import {
  adjustFirstTokenDisplaySeconds,
  parseFirstTokenDisplayConfig,
} from '../first-token-display'
import { resolveLogTiming } from '../format'

describe('usage log timing display', () => {
  test.each([
    [2.9, 2.9],
    [3, 1],
    [4.9, 2.9],
    [5, 1],
    [9, 5],
    [9.1, 4.55],
    [20, 10],
    [0, null],
    [-1, null],
  ])(
    'adjusts first-token display time: %s -> %s',
    (seconds: number, expected: number | null) => {
      const actual = adjustFirstTokenDisplaySeconds(seconds)
      if (expected == null) {
        expect(actual).toBeNull()
      } else {
        expect(actual).toBeCloseTo(expected, 10)
      }
    }
  )

  test('applies the first matching configured rule in display order', () => {
    const config = parseFirstTokenDisplayConfig({
      enabled: true,
      rules: [
        {
          id: 'first',
          comparison: 'gte',
          threshold: 5,
          operation: 'add',
          value: 1,
        },
        {
          id: 'second',
          comparison: 'gte',
          threshold: 3,
          operation: 'subtract',
          value: 2,
        },
      ],
    })

    expect(adjustFirstTokenDisplaySeconds(6, config)).toBe(7)
    expect(adjustFirstTokenDisplaySeconds(4, config)).toBe(2)
  })

  test('returns the recorded value when display adjustment is disabled', () => {
    const config = parseFirstTokenDisplayConfig({ enabled: false, rules: [] })

    expect(adjustFirstTokenDisplaySeconds(12, config)).toBe(12)
  })

  test('clamps configured subtraction at zero', () => {
    const config = parseFirstTokenDisplayConfig({
      enabled: true,
      rules: [
        {
          id: 'subtract-too-much',
          comparison: 'gte',
          threshold: 3,
          operation: 'subtract',
          value: 10,
        },
      ],
    })

    expect(adjustFirstTokenDisplaySeconds(3, config)).toBe(0)
  })

  test('falls back to default rules for malformed configuration', () => {
    const config = parseFirstTokenDisplayConfig('{invalid')

    expect(adjustFirstTokenDisplaySeconds(10, config)).toBe(5)
  })

  test('prefers attempt-scoped upstream timing for new logs', () => {
    expect(
      resolveLogTiming(31, {
        frt: 24100,
        upstream_frt: 19230,
        upstream_duration: 25.84,
      })
    ).toEqual({ durationSec: 25.84, frtMs: 19230 })
  })

  test('falls back to historical timing fields for old logs', () => {
    expect(resolveLogTiming(31, { frt: 24100 })).toEqual({
      durationSec: 31,
      frtMs: 24100,
    })
  })

  test('ignores invalid upstream timing values', () => {
    expect(
      resolveLogTiming(8, {
        frt: 1100,
        upstream_frt: Number.NaN,
        upstream_duration: -1,
      })
    ).toEqual({ durationSec: 8, frtMs: 1100 })
  })
})
