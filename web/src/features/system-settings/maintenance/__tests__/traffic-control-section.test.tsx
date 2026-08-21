import { Window } from 'happy-dom'
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
import { afterAll as after, assert, describe, test } from 'vitest'

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
const { TrafficControlSection } = await import('../traffic-control-section')

const i18n = createInstance()
await i18n.use(initReactI18next).init({
  lng: 'en',
  resources: {
    en: {
      translation: {
        'Block mainland China web traffic': 'Block mainland China web traffic',
        'Also block Hong Kong, Macau, and Taiwan':
          'Also block Hong Kong, Macau, and Taiwan',
        'When enabled, visitors identified as HK, MO, or TW will see the same warning page.':
          'When enabled, visitors identified as HK, MO, or TW will see the same warning page.',
        'Country header': 'Country header',
        'Automatic traffic control': 'Automatic traffic control',
        'Repeat every day': 'Repeat every day',
        'Schedule date': 'Schedule date',
        'Schedule timezone': 'Schedule timezone',
        'Time periods': 'Time periods',
        'Start time': 'Start time',
        'End time': 'End time',
        'Add time period': 'Add time period',
        'Remove time period': 'Remove time period',
        'Traffic Control': 'Traffic Control',
        'Warning page title': 'Warning page title',
        'Warning page content': 'Warning page content',
        'Warning page note': 'Warning page note',
      },
    },
  },
})

const reactTestGlobals = globalThis as typeof globalThis & {
  IS_REACT_ACT_ENVIRONMENT?: boolean
}
reactTestGlobals.IS_REACT_ACT_ENVIRONMENT = true

describe('traffic control settings', () => {
  after(() => {
    domWindow.close()
  })

  test('shows saved state and lets the operator edit traffic control settings', async () => {
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
            <TrafficControlSection
              defaultValues={{
                'traffic_control.mainland_web_block_enabled': false,
                'traffic_control.include_hong_kong_taiwan': false,
                'traffic_control.country_header': 'CF-IPCountry',
                'traffic_control.schedule':
                  '{"enabled":false,"daily_repeat":true,"date":"","time_ranges":[{"start_time":"09:00","end_time":"18:00"}],"timezone":"Asia/Shanghai"}',
                'traffic_control.warning_title': 'WEB ACCESS UNAVAILABLE',
                'traffic_control.warning_content':
                  'Mainland China IP addresses are not permitted to access web content.',
                'traffic_control.warning_annotation':
                  'This site has suspended access from mainland China and is available only to overseas users.',
              }}
            />
          </I18nextProvider>
        </QueryClientProvider>
      )
    })

    const restrictionSwitch =
      container.querySelector<HTMLElement>('[role="switch"]')
    assert.ok(restrictionSwitch)
    assert.equal(restrictionSwitch.getAttribute('aria-checked'), 'false')

    const regionCheckbox =
      container.querySelector<HTMLElement>('[role="checkbox"]')
    assert.ok(regionCheckbox)
    assert.equal(regionCheckbox.getAttribute('aria-checked'), 'false')

    const headerInput = container.querySelector<HTMLInputElement>(
      'input[name="traffic_control.country_header"]'
    )
    assert.ok(headerInput)
    assert.equal(headerInput.value, 'CF-IPCountry')

    const titleInput = container.querySelector<HTMLInputElement>(
      'input[name="traffic_control.warning_title"]'
    )
    assert.ok(titleInput)
    assert.equal(titleInput.value, 'WEB ACCESS UNAVAILABLE')

    const contentInput = container.querySelector<HTMLTextAreaElement>(
      'textarea[name="traffic_control.warning_content"]'
    )
    assert.ok(contentInput)
    assert.equal(
      contentInput.value,
      'Mainland China IP addresses are not permitted to access web content.'
    )

    const annotationInput = container.querySelector<HTMLTextAreaElement>(
      'textarea[name="traffic_control.warning_annotation"]'
    )
    assert.ok(annotationInput)
    assert.equal(
      annotationInput.value,
      'This site has suspended access from mainland China and is available only to overseas users.'
    )

    await act(async () => {
      restrictionSwitch.click()
      regionCheckbox.click()
      headerInput.value = 'X-Visitor-Country'
      headerInput.dispatchEvent(
        new domWindow.Event('input', { bubbles: true }) as unknown as Event
      )
      titleInput.value = 'CUSTOM TITLE'
      titleInput.dispatchEvent(
        new domWindow.Event('input', { bubbles: true }) as unknown as Event
      )
      contentInput.value = 'Custom content'
      contentInput.dispatchEvent(
        new domWindow.Event('input', { bubbles: true }) as unknown as Event
      )
      annotationInput.value = 'Custom note'
      annotationInput.dispatchEvent(
        new domWindow.Event('input', { bubbles: true }) as unknown as Event
      )
    })

    assert.equal(restrictionSwitch.getAttribute('aria-checked'), 'true')
    assert.equal(regionCheckbox.getAttribute('aria-checked'), 'true')
    assert.equal(headerInput.value, 'X-Visitor-Country')
    assert.equal(titleInput.value, 'CUSTOM TITLE')
    assert.equal(contentInput.value, 'Custom content')
    assert.equal(annotationInput.value, 'Custom note')

    const scheduleSwitches =
      container.querySelectorAll<HTMLElement>('[role="switch"]')
    assert.equal(scheduleSwitches.length, 3)
    assert.equal(scheduleSwitches[1].getAttribute('aria-checked'), 'false')
    assert.equal(scheduleSwitches[2].getAttribute('aria-checked'), 'true')

    const addTimePeriod = [
      ...container.querySelectorAll<HTMLButtonElement>('button'),
    ].find((button) => button.textContent?.includes('Add time period'))
    assert.ok(addTimePeriod)
    await act(async () => addTimePeriod.click())
    assert.equal(container.querySelectorAll('input[type="time"]').length, 4)

    await act(async () => scheduleSwitches[2].click())
    assert.ok(container.querySelector('input[type="date"]'))

    await act(async () => root.unmount())
    container.remove()
    queryClient.clear()
  })
})
