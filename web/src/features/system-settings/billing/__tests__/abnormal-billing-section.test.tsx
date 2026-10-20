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
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { render, screen, waitFor } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { createInstance, type i18n } from 'i18next'
import { useState } from 'react'
import { I18nextProvider } from 'react-i18next'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'

import { api } from '@/lib/api'

import { SettingsPageProvider } from '../../components/settings-page-context'
import {
  AbnormalBillingSection,
  type AbnormalBillingValues,
} from '../abnormal-billing-section'

const allCharged: AbnormalBillingValues = {
  align_with_official: false,
  charge_client_disconnect_before_response: true,
  charge_client_disconnect_during_response: true,
  charge_upstream_interrupt_during_response: true,
  continue_after_cloudflare_truncation: true,
  match_upstream_charge: false,
}

const alignLabel = 'Align abnormal billing with official new-api'
const beforeLabel =
  'Charge when the client disconnects before upstream responds'
const duringLabel =
  'Charge when the client disconnects while receiving the response'
const upstreamLabel =
  'Charge when upstream or the gateway interrupts the response midway'
const cloudflareLabel =
  'Keep reading upstream after a Cloudflare truncation and charge actual usage'

let testI18n: i18n

function Fixture(props: { values: AbnormalBillingValues }) {
  const [container, setContainer] = useState<HTMLDivElement | null>(null)
  const [client] = useState(
    () => new QueryClient({ defaultOptions: { mutations: { retry: false } } })
  )
  return (
    <I18nextProvider i18n={testI18n}>
      <QueryClientProvider client={client}>
        <div ref={setContainer} />
        <SettingsPageProvider actionsContainer={container}>
          <AbnormalBillingSection defaultValues={props.values} />
        </SettingsPageProvider>
      </QueryClientProvider>
    </I18nextProvider>
  )
}

beforeEach(async () => {
  testI18n = createInstance()
  await testI18n.init({
    lng: 'en',
    resources: { en: { translation: {} } },
    interpolation: { escapeValue: false },
  })
})

afterEach(() => {
  vi.restoreAllMocks()
})

describe('abnormal billing control section', () => {
  it('shows every switch with its saved value and nothing to save', () => {
    render(
      <Fixture
        values={{
          ...allCharged,
          charge_upstream_interrupt_during_response: false,
        }}
      />
    )

    expect(screen.getByRole('switch', { name: alignLabel })).not.toBeChecked()
    expect(screen.getByRole('switch', { name: beforeLabel })).toBeChecked()
    expect(
      screen.getByRole('switch', { name: beforeLabel })
    ).not.toHaveAttribute('aria-disabled', 'true')
    expect(screen.getByRole('switch', { name: duringLabel })).toBeChecked()
    expect(
      screen.getByRole('switch', { name: upstreamLabel })
    ).not.toBeChecked()
    expect(
      screen.getByRole('button', { name: 'Save abnormal billing settings' })
    ).toBeDisabled()
  })

  it('saves only the toggled switch and returns to a clean state', async () => {
    const put = vi
      .spyOn(api, 'put')
      .mockResolvedValue({ data: { success: true } })
    const user = userEvent.setup()
    render(<Fixture values={allCharged} />)

    await user.click(screen.getByRole('switch', { name: duringLabel }))
    const save = screen.getByRole('button', {
      name: 'Save abnormal billing settings',
    })
    expect(save).toBeEnabled()
    await user.click(save)

    await waitFor(() => expect(save).toBeDisabled())
    expect(put).toHaveBeenCalledTimes(1)
    expect(put).toHaveBeenCalledWith('/api/option/', {
      key: 'abnormal_billing_setting.charge_client_disconnect_during_response',
      value: 'false',
    })
    expect(screen.getByRole('switch', { name: duringLabel })).not.toBeChecked()
  })

  it('keeps a rejected switch dirty instead of reporting it as saved', async () => {
    vi.spyOn(api, 'put').mockResolvedValue({
      data: { success: false, message: 'must be true or false' },
    })
    const user = userEvent.setup()
    render(<Fixture values={allCharged} />)

    await user.click(screen.getByRole('switch', { name: beforeLabel }))
    const save = screen.getByRole('button', {
      name: 'Save abnormal billing settings',
    })
    await user.click(save)

    await waitFor(() => expect(save).toBeEnabled())
    expect(screen.getByRole('switch', { name: beforeLabel })).not.toBeChecked()
  })

  it('disables the three case switches while the official alignment is on', () => {
    render(<Fixture values={{ ...allCharged, align_with_official: true }} />)

    expect(screen.getByRole('switch', { name: alignLabel })).toBeChecked()
    expect(
      screen.getByRole('switch', { name: alignLabel })
    ).not.toHaveAttribute('aria-disabled', 'true')
    for (const label of [beforeLabel, duringLabel, upstreamLabel]) {
      expect(screen.getByRole('switch', { name: label })).toHaveAttribute(
        'aria-disabled',
        'true'
      )
    }
  })

  it('keeps the Cloudflare switch usable and saves it alone while the official alignment is on', async () => {
    const put = vi
      .spyOn(api, 'put')
      .mockResolvedValue({ data: { success: true } })
    const user = userEvent.setup()
    render(<Fixture values={{ ...allCharged, align_with_official: true }} />)

    const cloudflare = screen.getByRole('switch', { name: cloudflareLabel })
    expect(cloudflare).toBeChecked()
    expect(cloudflare).not.toHaveAttribute('aria-disabled', 'true')
    await user.click(cloudflare)
    const save = screen.getByRole('button', {
      name: 'Save abnormal billing settings',
    })
    await user.click(save)

    await waitFor(() => expect(save).toBeDisabled())
    expect(put).toHaveBeenCalledTimes(1)
    expect(put).toHaveBeenCalledWith('/api/option/', {
      key: 'abnormal_billing_setting.continue_after_cloudflare_truncation',
      value: 'false',
    })
  })

  it('saves the upstream match switch alone and keeps it usable under the official alignment', async () => {
    const put = vi
      .spyOn(api, 'put')
      .mockResolvedValue({ data: { success: true } })
    const user = userEvent.setup()
    render(<Fixture values={{ ...allCharged, align_with_official: true }} />)

    const match = screen.getByRole('switch', {
      name: 'Charge whatever upstream billed for abandoned requests',
    })
    expect(match).not.toBeChecked()
    expect(match).not.toHaveAttribute('aria-disabled', 'true')
    await user.click(match)
    const save = screen.getByRole('button', {
      name: 'Save abnormal billing settings',
    })
    await user.click(save)

    await waitFor(() => expect(save).toBeDisabled())
    expect(put).toHaveBeenCalledTimes(1)
    expect(put).toHaveBeenCalledWith('/api/option/', {
      key: 'abnormal_billing_setting.match_upstream_charge',
      value: 'true',
    })
  })

  it('saves the official alignment and locks the case switches once it is turned on', async () => {
    const put = vi
      .spyOn(api, 'put')
      .mockResolvedValue({ data: { success: true } })
    const user = userEvent.setup()
    render(<Fixture values={allCharged} />)

    expect(
      screen.getByRole('switch', { name: beforeLabel })
    ).not.toHaveAttribute('aria-disabled', 'true')
    await user.click(screen.getByRole('switch', { name: alignLabel }))
    expect(screen.getByRole('switch', { name: beforeLabel })).toHaveAttribute(
      'aria-disabled',
      'true'
    )
    const save = screen.getByRole('button', {
      name: 'Save abnormal billing settings',
    })
    await user.click(save)

    await waitFor(() => expect(save).toBeDisabled())
    expect(put).toHaveBeenCalledTimes(1)
    expect(put).toHaveBeenCalledWith('/api/option/', {
      key: 'abnormal_billing_setting.align_with_official',
      value: 'true',
    })
  })

  it('keeps the accepted switch saved when a later one is rejected', async () => {
    const put = vi
      .spyOn(api, 'put')
      .mockResolvedValueOnce({ data: { success: true } })
      .mockResolvedValueOnce({ data: { success: false, message: 'rejected' } })
    const user = userEvent.setup()
    render(<Fixture values={allCharged} />)

    await user.click(screen.getByRole('switch', { name: beforeLabel }))
    await user.click(screen.getByRole('switch', { name: upstreamLabel }))
    const save = screen.getByRole('button', {
      name: 'Save abnormal billing settings',
    })
    await user.click(save)

    await waitFor(() => expect(put).toHaveBeenCalledTimes(2))
    await waitFor(() => expect(save).toBeEnabled())
    // Undoing only the rejected switch leaves nothing to save, which holds only
    // if the accepted switch became the new saved baseline.
    await user.click(screen.getByRole('switch', { name: upstreamLabel }))
    expect(screen.getByRole('switch', { name: beforeLabel })).not.toBeChecked()
    expect(save).toBeDisabled()
  })
})
