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
import { assert, describe, it } from 'vitest'

import {
  buildModelSnapshots,
  getPriceSummary,
} from '../model-pricing-snapshots'

describe('per-second model pricing snapshots', () => {
  it('keeps the per-second mode and displays the fixed price by second', () => {
    const [snapshot] = buildModelSnapshots({
      modelPrice: '{"video-model":0.3}',
      modelRatio: '{}',
      cacheRatio: '{}',
      createCacheRatio: '{}',
      completionRatio: '{}',
      imageRatio: '{}',
      audioRatio: '{}',
      audioCompletionRatio: '{}',
      billingMode: '{"video-model":"per_second"}',
      billingExpr: '{}',
    })

    assert.equal(snapshot.name, 'video-model')
    assert.equal(snapshot.billingMode, 'per-second')
    assert.equal(snapshot.price, '0.3')
    assert.equal(
      getPriceSummary(snapshot, (key) => key),
      '$0.3 / second'
    )
  })
})
