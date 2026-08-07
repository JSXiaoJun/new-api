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

const ACCESS_CHECK_PATH = '/web-access-denied?check=1'
const ACCESS_DENIED_PATH = '/web-access-denied'
const ACCESS_CHECK_INTERVAL_MS = 10_000
const ACCESS_CHECK_TIMEOUT_MS = 5_000
const TRAFFIC_CONTROL_HEADER = 'X-Traffic-Control'

type GuardListener = () => void

export type TrafficAccessGuardRuntime = {
  getDeniedURL: () => Promise<string | null>
  redirect: (path: string) => void
  setInterval: (callback: GuardListener, delay: number) => number
  clearInterval: (intervalId: number) => void
  addWindowListener: (type: 'focus' | 'online', listener: GuardListener) => void
  removeWindowListener: (
    type: 'focus' | 'online',
    listener: GuardListener
  ) => void
  addVisibilityListener: (listener: GuardListener) => void
  removeVisibilityListener: (listener: GuardListener) => void
  isVisible: () => boolean
}

export type TrafficAccessCheckRuntime = {
  currentOrigin: string
  readServerAddress: () => string | null
  checkOrigin: (
    origin: string,
    mode: 'same-origin' | 'cross-origin-image'
  ) => Promise<boolean>
}

export async function checkTrafficAccess(
  runtime: TrafficAccessCheckRuntime
): Promise<string | null> {
  try {
    if (await runtime.checkOrigin(runtime.currentOrigin, 'same-origin')) {
      return `${runtime.currentOrigin}${ACCESS_DENIED_PATH}`
    }
  } catch {
    // The primary-origin check below may still be reachable.
  }

  const serverAddress = runtime.readServerAddress()
  if (!serverAddress) return null

  try {
    const canonicalOrigin = new URL(serverAddress).origin
    if (canonicalOrigin === runtime.currentOrigin) return null
    try {
      const blocked = await runtime.checkOrigin(
        canonicalOrigin,
        'cross-origin-image'
      )
      return blocked ? `${canonicalOrigin}${ACCESS_DENIED_PATH}` : null
    } catch {
      return null
    }
  } catch {
    return null
  }
}

export function startTrafficAccessGuard(
  runtime: TrafficAccessGuardRuntime
): () => void {
  let stopped = false
  let checking = false

  const checkAccess = async () => {
    if (stopped || checking) return

    checking = true
    try {
      const deniedURL = await runtime.getDeniedURL()
      if (deniedURL) {
        stopped = true
        runtime.redirect(deniedURL)
      }
    } catch {
      // A temporary network failure must not replace the current page.
    } finally {
      checking = false
    }
  }

  const checkWhenVisible = () => {
    if (runtime.isVisible()) void checkAccess()
  }

  void checkAccess()
  const intervalId = runtime.setInterval(
    checkWhenVisible,
    ACCESS_CHECK_INTERVAL_MS
  )
  runtime.addWindowListener('focus', checkAccess)
  runtime.addWindowListener('online', checkAccess)
  runtime.addVisibilityListener(checkWhenVisible)

  return () => {
    stopped = true
    runtime.clearInterval(intervalId)
    runtime.removeWindowListener('focus', checkAccess)
    runtime.removeWindowListener('online', checkAccess)
    runtime.removeVisibilityListener(checkWhenVisible)
  }
}

let installed = false

export function installTrafficAccessGuard(): void {
  if (installed) return
  if (typeof window === 'undefined' || typeof document === 'undefined') return
  installed = true

  const checkOrigin = async (
    origin: string,
    mode: 'same-origin' | 'cross-origin-image'
  ): Promise<boolean> => {
    const cacheBuster = Date.now().toString()
    if (mode === 'same-origin') {
      const response = await window.fetch(
        `${origin}${ACCESS_CHECK_PATH}&_=${cacheBuster}`,
        {
          cache: 'no-store',
          credentials: 'same-origin',
        }
      )
      return (
        response.status === 403 &&
        response.headers.get(TRAFFIC_CONTROL_HEADER) === 'blocked'
      )
    }

    return new Promise((resolve) => {
      const image = new Image()
      let settled = false
      let timeoutId = 0
      const handleLoad = () => finish(true)
      const handleError = () => finish(false)
      const finish = (blocked: boolean) => {
        if (settled) return
        settled = true
        window.clearTimeout(timeoutId)
        image.removeEventListener('load', handleLoad)
        image.removeEventListener('error', handleError)
        resolve(blocked)
      }
      timeoutId = window.setTimeout(
        () => finish(false),
        ACCESS_CHECK_TIMEOUT_MS
      )
      image.addEventListener('load', handleLoad, { once: true })
      image.addEventListener('error', handleError, { once: true })
      image.src = `${origin}${ACCESS_DENIED_PATH}?check=image&_=${cacheBuster}`
    })
  }

  startTrafficAccessGuard({
    getDeniedURL: () =>
      checkTrafficAccess({
        currentOrigin: window.location.origin,
        readServerAddress: () => {
          try {
            const status = JSON.parse(
              window.localStorage.getItem('status') ?? '{}'
            ) as {
              server_address?: unknown
            }
            return typeof status.server_address === 'string'
              ? status.server_address
              : null
          } catch {
            return null
          }
        },
        checkOrigin,
      }),
    redirect: (path) => window.location.replace(path),
    setInterval: (callback, delay) => window.setInterval(callback, delay),
    clearInterval: (intervalId) => window.clearInterval(intervalId),
    addWindowListener: (type, listener) =>
      window.addEventListener(type, listener),
    removeWindowListener: (type, listener) =>
      window.removeEventListener(type, listener),
    addVisibilityListener: (listener) =>
      document.addEventListener('visibilitychange', listener),
    removeVisibilityListener: (listener) =>
      document.removeEventListener('visibilitychange', listener),
    isVisible: () => document.visibilityState === 'visible',
  })
}
