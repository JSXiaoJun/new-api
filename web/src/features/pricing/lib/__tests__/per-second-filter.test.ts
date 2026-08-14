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
import assert from 'node:assert/strict'
import { describe, it } from 'node:test'

import { QUOTA_TYPES } from '../../constants'
import type { PricingModel } from '../../types'
import { filterByQuotaType } from '../filters'

const fixedModel = (name: string, billingMode?: string): PricingModel => ({
  id: 1,
  model_name: name,
  quota_type: 1,
  model_ratio: 0,
  completion_ratio: 0,
  model_price: 0.3,
  enable_groups: ['default'],
  billing_mode: billingMode,
})

describe('per-second pricing filter', () => {
  it('separates per-second models from fixed per-request models', () => {
    const models = [
      fixedModel('request-model'),
      fixedModel('second-model', 'per_second'),
    ]

    assert.deepEqual(
      filterByQuotaType(models, QUOTA_TYPES.REQUEST).map(
        (model) => model.model_name
      ),
      ['request-model']
    )
    assert.deepEqual(
      filterByQuotaType(models, QUOTA_TYPES.SECOND).map(
        (model) => model.model_name
      ),
      ['second-model']
    )
  })
})
