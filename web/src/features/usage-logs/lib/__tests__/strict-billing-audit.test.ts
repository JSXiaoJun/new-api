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
import { createInstance } from 'i18next'
import { describe, expect, test } from 'vitest'

import zh from '@/i18n/locales/zh.json'

import { renderAuditContent } from '../format'

describe('strict billing audit content', () => {
  test.each([
    [true, '已为用户 abuser（ID: 42）开启严格计费'],
    [false, '已为用户 abuser（ID: 42）关闭严格计费'],
  ])(
    'enabled=%s is described in words, not as a raw boolean',
    async (enabled, expected) => {
      const i18n = createInstance()
      await i18n.init({ lng: 'zh', resources: { zh } })

      expect(
        renderAuditContent(
          {
            op: {
              action: 'user.strict_billing',
              params: { username: 'abuser', id: 42, enabled },
            },
          },
          i18n.t
        )
      ).toBe(expected)
    }
  )
})
