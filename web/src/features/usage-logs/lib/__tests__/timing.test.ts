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

import { resolveLogTiming } from '../format'

describe('usage log timing display', () => {
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
