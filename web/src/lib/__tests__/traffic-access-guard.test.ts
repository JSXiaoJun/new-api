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

import {
  checkTrafficAccess,
  startTrafficAccessGuard,
} from '../traffic-access-guard'

describe('traffic access guard', () => {
  test('uses the configured primary origin when the CDN cannot identify the country', async () => {
    const checks: Array<[string, string]> = []

    const deniedURL = await checkTrafficAccess({
      currentOrigin: 'https://cdn.example.com',
      readServerAddress: () => 'https://www.example.com/path',
      checkOrigin: async (origin, mode) => {
        checks.push([origin, mode])
        return origin === 'https://www.example.com' ? 'blocked' : 'unavailable'
      },
    })

    assert.equal(deniedURL, 'https://www.example.com/web-access-denied')
    assert.deepEqual(checks, [
      ['https://cdn.example.com', 'same-origin'],
      ['https://www.example.com', 'cross-origin-image'],
    ])
  })

  test('does not repeat the country check when the configured origin is current', async () => {
    const checks: Array<[string, string]> = []

    const deniedURL = await checkTrafficAccess({
      currentOrigin: 'https://www.example.com',
      readServerAddress: () => 'https://www.example.com',
      checkOrigin: async (origin, mode) => {
        checks.push([origin, mode])
        return 'unavailable'
      },
    })

    assert.equal(deniedURL, null)
    assert.deepEqual(checks, [['https://www.example.com', 'same-origin']])
  })

  test('does not call the primary origin when the CDN evaluated the visitor as allowed', async () => {
    const checks: Array<[string, string]> = []

    const deniedURL = await checkTrafficAccess({
      currentOrigin: 'https://cdn.example.com',
      readServerAddress: () => 'https://www.example.com',
      checkOrigin: async (origin, mode) => {
        checks.push([origin, mode])
        return 'allowed'
      },
    })

    assert.equal(deniedURL, null)
    assert.deepEqual(checks, [['https://cdn.example.com', 'same-origin']])
  })

  test('redirects immediately when the current origin identifies blocked traffic', async () => {
    const checks: Array<[string, string]> = []

    const deniedURL = await checkTrafficAccess({
      currentOrigin: 'https://cdn.example.com',
      readServerAddress: () => 'https://www.example.com',
      checkOrigin: async (origin, mode) => {
        checks.push([origin, mode])
        return 'blocked'
      },
    })

    assert.equal(deniedURL, 'https://cdn.example.com/web-access-denied')
    assert.deepEqual(checks, [['https://cdn.example.com', 'same-origin']])
  })

  test('redirects an already loaded dashboard when a later check is blocked', async () => {
    const blockedChecks = [false, true]
    const redirects: string[] = []
    let intervalCallback: (() => void) | undefined
    let intervalDelay = 0

    const stop = startTrafficAccessGuard({
      getDeniedURL: async () =>
        blockedChecks.shift()
          ? 'https://www.example.com/web-access-denied'
          : null,
      redirect: (path) => redirects.push(path),
      setInterval: (callback, delay) => {
        intervalCallback = callback
        intervalDelay = delay
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
    assert.equal(intervalDelay, 30_000)

    intervalCallback?.()
    await Promise.resolve()
    assert.deepEqual(redirects, ['https://www.example.com/web-access-denied'])

    intervalCallback?.()
    await Promise.resolve()
    assert.deepEqual(redirects, ['https://www.example.com/web-access-denied'])

    stop()
  })

  test('checks immediately when a hidden dashboard becomes visible again', async () => {
    let visible = false
    let checks = 0
    let visibilityCallback: (() => void) | undefined

    const stop = startTrafficAccessGuard({
      getDeniedURL: async () => {
        checks += 1
        return null
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

  test('stopping the guard removes its polling and event listeners', () => {
    const removedWindowListeners: string[] = []
    let clearedInterval = 0
    let visibilityListenerRemoved = false

    const stop = startTrafficAccessGuard({
      getDeniedURL: async () => null,
      redirect: () => undefined,
      setInterval: () => 42,
      clearInterval: (intervalId) => {
        clearedInterval = intervalId
      },
      addWindowListener: () => undefined,
      removeWindowListener: (type) => removedWindowListeners.push(type),
      addVisibilityListener: () => undefined,
      removeVisibilityListener: () => {
        visibilityListenerRemoved = true
      },
      isVisible: () => true,
    })

    stop()

    assert.equal(clearedInterval, 42)
    assert.deepEqual(removedWindowListeners, ['focus', 'online'])
    assert.equal(visibilityListenerRemoved, true)
  })
})
