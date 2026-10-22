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

import { buildMinuteSlots } from '../lib/minutes'

describe('minute slots', () => {
  test('fills every minute of the window, oldest first', () => {
    const slots = buildMinuteSlots(0, 180, 60, [
      { ts: 60, success_rate: 50 },
      { ts: 120, success_rate: null },
    ])

    expect(slots).toEqual([
      { ts: 0, state: 'idle' },
      { ts: 60, state: 'rated', successRate: 50 },
      { ts: 120, state: 'sparse' },
      { ts: 180, state: 'idle' },
    ])
  })

  test('keeps a zero success rate as a rated minute', () => {
    expect(buildMinuteSlots(0, 0, 60, [{ ts: 0, success_rate: 0 }])).toEqual([
      { ts: 0, state: 'rated', successRate: 0 },
    ])
  })

  test('returns no slots for an invalid window', () => {
    expect(buildMinuteSlots(120, 60, 60, [])).toEqual([])
    expect(buildMinuteSlots(0, 60, 0, [])).toEqual([])
  })
})
