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

import { parseHeaderNavModules } from '../config'

describe('header navigation config', () => {
  test('keeps rankings public for legacy configurations', () => {
    const config = parseHeaderNavModules(
      JSON.stringify({ rankings: { enabled: true, requireAuth: false } })
    )

    assert.deepEqual(config.rankings, {
      enabled: true,
      requireAuth: false,
      adminOnly: false,
    })
  })

  test('parses the rankings administrator-only setting', () => {
    const config = parseHeaderNavModules(
      JSON.stringify({
        rankings: {
          enabled: true,
          requireAuth: false,
          adminOnly: '1',
        },
      })
    )

    assert.equal(config.rankings.adminOnly, true)
  })
})
