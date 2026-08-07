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
import { describe, test } from 'node:test'

import { startTrafficAccessGuard } from '../traffic-access-guard'

describe('traffic access guard', () => {
  test('redirects an already loaded dashboard when a later check is blocked', async () => {
    const blockedChecks = [false, true]
    const redirects: string[] = []
    let intervalCallback: (() => void) | undefined

    const stop = startTrafficAccessGuard({
      checkBlocked: async () => blockedChecks.shift() ?? false,
      redirect: (path) => redirects.push(path),
      setInterval: (callback) => {
        intervalCallback = callback
        return 1
      },
      clearInterval: () => undefined,
      addWindowListener: () => undefined,
      removeWindowListener: () => undefined,
      addVisibilityListener: () => undefined,
      removeVisibilityListener: () => undefined,
      isVisible: () => true,
    })

    await Promise.resolve()
    assert.deepEqual(redirects, [])

    intervalCallback?.()
    await Promise.resolve()
    assert.deepEqual(redirects, ['/web-access-denied'])

    intervalCallback?.()
    await Promise.resolve()
    assert.deepEqual(redirects, ['/web-access-denied'])

    stop()
  })

  test('checks immediately when a hidden dashboard becomes visible again', async () => {
    let visible = false
    let checks = 0
    let visibilityCallback: (() => void) | undefined

    const stop = startTrafficAccessGuard({
      checkBlocked: async () => {
        checks += 1
        return false
      },
      redirect: () => undefined,
      setInterval: () => 1,
      clearInterval: () => undefined,
      addWindowListener: () => undefined,
      removeWindowListener: () => undefined,
      addVisibilityListener: (callback) => {
        visibilityCallback = callback
      },
      removeVisibilityListener: () => undefined,
      isVisible: () => visible,
    })

    await Promise.resolve()
    assert.equal(checks, 1)

    visible = true
    visibilityCallback?.()
    await Promise.resolve()
    assert.equal(checks, 2)

    stop()
  })
})
