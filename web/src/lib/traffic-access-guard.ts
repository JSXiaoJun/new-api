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
const TRAFFIC_CONTROL_HEADER = 'X-Traffic-Control'

type GuardListener = () => void

export type TrafficAccessGuardRuntime = {
  checkBlocked: () => Promise<boolean>
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

export function startTrafficAccessGuard(
  runtime: TrafficAccessGuardRuntime
): () => void {
  let stopped = false
  let checking = false

  const checkAccess = async () => {
    if (stopped || checking) return

    checking = true
    try {
      if (await runtime.checkBlocked()) {
        stopped = true
        runtime.redirect(ACCESS_DENIED_PATH)
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

  startTrafficAccessGuard({
    checkBlocked: async () => {
      const response = await window.fetch(ACCESS_CHECK_PATH, {
        cache: 'no-store',
        credentials: 'same-origin',
      })
      return (
        response.status === 403 &&
        response.headers.get(TRAFFIC_CONTROL_HEADER) === 'blocked'
      )
    },
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
