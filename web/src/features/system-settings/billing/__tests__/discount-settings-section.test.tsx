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
import { after, describe, test } from 'node:test'

import { Window } from 'happy-dom'

const domWindow = new Window()
const domGlobals = [
  'window',
  'document',
  'navigator',
  'HTMLElement',
  'HTMLInputElement',
  'SVGElement',
  'Node',
  'Element',
  'Event',
  'CustomEvent',
  'MutationObserver',
  'requestAnimationFrame',
  'cancelAnimationFrame',
  'getComputedStyle',
] as const

for (const key of domGlobals) {
  Object.defineProperty(globalThis, key, {
    configurable: true,
    value: domWindow[key],
  })
}

const { act } = await import('react')
const { createRoot } = await import('react-dom/client')
const { QueryClient, QueryClientProvider } =
  await import('@tanstack/react-query')
const { createInstance } = await import('i18next')
const { I18nextProvider, initReactI18next } = await import('react-i18next')
const { api } = await import('@/lib/api')
const { SettingsPageProvider } =
  await import('../../components/settings-page-context')
const { DiscountSettingsSection } = await import('../discount-settings-section')
const { createDiscountSettingsSchema } = await import('../discount-schedule')

const i18n = createInstance()
await i18n
  .use(initReactI18next)
  .init({ lng: 'en', resources: { en: { translation: {} } } })

const reactTestGlobals = globalThis as typeof globalThis & {
  IS_REACT_ACT_ENVIRONMENT?: boolean
}
reactTestGlobals.IS_REACT_ACT_ENVIRONMENT = true

type MockableApi = {
  put: (url: string, data?: unknown) => Promise<{ data: unknown }>
}

const apiClient = api as unknown as MockableApi
const originalPut = apiClient.put

async function waitForCondition(check: () => boolean): Promise<void> {
  const deadline = Date.now() + 1000
  while (!check()) {
    if (Date.now() >= deadline) throw new Error('condition was not met')
    await new Promise((resolve) => setTimeout(resolve, 0))
  }
}

function setInputValue(input: HTMLInputElement, value: string): void {
  const setter = Object.getOwnPropertyDescriptor(
    HTMLInputElement.prototype,
    'value'
  )?.set
  assert.ok(setter)
  setter.call(input, value)
  input.dispatchEvent(new Event('input', { bubbles: true }))
}

describe('discount settings section', () => {
  after(() => {
    apiClient.put = originalPut
    domWindow.close()
  })

  test('selects every user group and switches to a daily cross-midnight window', async () => {
    const container = document.createElement('div')
    document.body.append(container)
    const root = createRoot(container)
    const queryClient = new QueryClient({
      defaultOptions: { mutations: { retry: false } },
    })

    await act(async () => {
      root.render(
        <QueryClientProvider client={queryClient}>
          <I18nextProvider i18n={i18n}>
            <DiscountSettingsSection
              scheduleValue='{"enabled":false,"ratio":1,"groups":[],"daily_repeat":false,"start_at":"2026-08-08T00:00","end_at":"2026-08-09T00:00","start_time":"22:00","end_time":"06:00","timezone":"Asia/Shanghai"}'
              groupRatioValue='{"default":1,"vip":1.5}'
              usableGroupsValue='{"default":"Default group","vip":"VIP group"}'
            />
          </I18nextProvider>
        </QueryClientProvider>
      )
    })

    const buttons = [...container.querySelectorAll<HTMLButtonElement>('button')]
    const selectAll = buttons.find((button) =>
      button.textContent?.includes('Select all')
    )
    assert.ok(selectAll)

    await act(async () => selectAll.click())
    assert.equal(
      container.querySelectorAll('[data-slot="combobox-chip"]').length,
      2
    )

    const switches = container.querySelectorAll<HTMLElement>('[role="switch"]')
    assert.equal(switches.length, 2)
    assert.equal(switches[1].getAttribute('aria-checked'), 'false')

    await act(async () => switches[1].click())
    assert.equal(switches[1].getAttribute('aria-checked'), 'true')
    assert.ok(container.querySelector('input[name="start_time"]'))
    assert.ok(container.querySelector('input[name="end_time"]'))
    assert.equal(container.querySelector('input[name="start_at"]'), null)

    await act(async () => root.unmount())
    container.remove()
    queryClient.clear()
  })

  test('keeps rejected settings dirty instead of reporting them as saved', async () => {
    apiClient.put = async () => ({
      data: { success: false, message: 'invalid timezone' },
    })

    const container = document.createElement('div')
    const actionsContainer = document.createElement('div')
    document.body.append(container)
    document.body.append(actionsContainer)
    const root = createRoot(container)
    const queryClient = new QueryClient({
      defaultOptions: { mutations: { retry: false } },
    })

    await act(async () => {
      root.render(
        <QueryClientProvider client={queryClient}>
          <I18nextProvider i18n={i18n}>
            <SettingsPageProvider actionsContainer={actionsContainer}>
              <DiscountSettingsSection
                scheduleValue='{"enabled":false,"ratio":1,"groups":[],"daily_repeat":false,"start_at":"2026-08-08T00:00","end_at":"2026-08-09T00:00","start_time":"22:00","end_time":"06:00","timezone":"Asia/Shanghai"}'
                groupRatioValue='{"default":1}'
                usableGroupsValue='{"default":"Default group"}'
              />
            </SettingsPageProvider>
          </I18nextProvider>
        </QueryClientProvider>
      )
    })

    const timezoneInput = container.querySelector<HTMLInputElement>(
      'input[name="timezone"]'
    )
    assert.ok(timezoneInput)
    await act(async () => {
      setInputValue(timezoneInput, 'Invalid/Timezone')
    })

    const saveButton = [
      ...actionsContainer.querySelectorAll<HTMLButtonElement>('button'),
    ].find((button) => button.textContent?.includes('Save discount settings'))
    assert.ok(saveButton)
    assert.equal(saveButton.disabled, false)

    await act(async () => {
      saveButton.click()
      await waitForCondition(() => !saveButton.disabled)
    })

    assert.equal(timezoneInput.value, 'Invalid/Timezone')
    assert.equal(saveButton.disabled, false)

    await act(async () => root.unmount())
    container.remove()
    actionsContainer.remove()
    queryClient.clear()
    apiClient.put = originalPut
  })

  test('rejects a discount multiplier below the billing safety minimum', () => {
    const schema = createDiscountSettingsSchema((key) => key)
    const result = schema.safeParse({
      enabled: false,
      ratio: 0.009,
      groups: [],
      daily_repeat: false,
      start_at: '',
      end_at: '',
      start_time: '22:00',
      end_time: '06:00',
      timezone: 'Asia/Shanghai',
    })

    assert.equal(result.success, false)
    assert.equal(
      result.error?.issues[0]?.message,
      'Discount multiplier cannot be less than 0.01'
    )
  })
})
