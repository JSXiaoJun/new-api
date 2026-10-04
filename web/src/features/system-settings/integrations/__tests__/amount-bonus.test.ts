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
  getAmountBonusError,
  parseAmountBonusTiers,
  serializeAmountBonusTiers,
} from '../amount-bonus'

describe('top-up bonus tiers', () => {
  test('parses stored tiers sorted by threshold', () => {
    assert.deepEqual(parseAmountBonusTiers('{"500":20,"100":10}'), [
      { amount: 100, bonusPercent: 10 },
      { amount: 500, bonusPercent: 20 },
    ])
  })

  test('treats an empty value as no tiers', () => {
    assert.deepEqual(parseAmountBonusTiers(''), [])
    assert.equal(getAmountBonusError(''), null)
  })

  test('rejects values the server would refuse', () => {
    assert.equal(getAmountBonusError('{"100":10,"500":20.5}'), null)
    assert.equal(
      getAmountBonusError('[10]'),
      'Top-up bonus must be a JSON object'
    )
    for (const invalid of [
      '{"100":0}',
      '{"100":-5}',
      '{"0":10}',
      '{"10.5":10}',
      '{"100":1001}',
      '{"abc":10}',
      '{"1e2":10}',
      '{"100.0":10}',
      '{" 100":10}',
      '{"0x10":10}',
      '{"100":"10"}',
      '{"100":true}',
      '{"1000000000000000000000":10}',
    ]) {
      assert.isNotNull(getAmountBonusError(invalid), invalid)
    }
  })

  test('serializes tiers back to the stored JSON shape', () => {
    const serialized = serializeAmountBonusTiers([
      { amount: 500, bonusPercent: 20 },
      { amount: 100, bonusPercent: 10 },
    ])
    assert.deepEqual(JSON.parse(serialized), { '100': 10, '500': 20 })
    assert.deepEqual(parseAmountBonusTiers(serialized), [
      { amount: 100, bonusPercent: 10 },
      { amount: 500, bonusPercent: 20 },
    ])
  })
})
