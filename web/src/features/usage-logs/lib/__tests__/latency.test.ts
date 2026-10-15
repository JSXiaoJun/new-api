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

import { durationSeverity, firstTokenSeverity } from '../latency'

describe('usage log latency tiers', () => {
  test.each([
    [0, 'good'],
    [9.999, 'good'],
    [10, 'warn'],
    [29.999, 'warn'],
    [30, 'slow'],
    [59.999, 'slow'],
    [60, 'critical'],
  ] as const)('first token %ss is %s', (seconds, expected) => {
    expect(firstTokenSeverity(seconds)).toBe(expected)
  })

  test.each([
    [0, 'good'],
    [59.999, 'good'],
    [60, 'warn'],
    [179.999, 'warn'],
    [180, 'slow'],
    [299.999, 'slow'],
    [300, 'critical'],
  ] as const)('total duration %ss is %s', (seconds, expected) => {
    expect(durationSeverity(seconds)).toBe(expected)
  })
})
