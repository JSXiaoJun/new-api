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

import { getTopUpBonusPercent } from '../format'

describe('getTopUpBonusPercent', () => {
  const tiers = { 50: 5, 100: 10, 500: 20 }

  test('returns 0 when no tiers are configured', () => {
    expect(getTopUpBonusPercent(1000, undefined)).toBe(0)
    expect(getTopUpBonusPercent(1000, {})).toBe(0)
  })

  test('returns 0 below the lowest threshold', () => {
    expect(getTopUpBonusPercent(49, tiers)).toBe(0)
  })

  test('applies a tier once the amount reaches its threshold', () => {
    expect(getTopUpBonusPercent(50, tiers)).toBe(5)
    expect(getTopUpBonusPercent(499, tiers)).toBe(10)
    expect(getTopUpBonusPercent(500, tiers)).toBe(20)
    expect(getTopUpBonusPercent(10_000, tiers)).toBe(20)
  })

  test('ignores tiers the server would ignore', () => {
    expect(getTopUpBonusPercent(1000, { 0: 50, 100: -5, 200: 5000 })).toBe(0)
  })
})
