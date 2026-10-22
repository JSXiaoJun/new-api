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

const defaults: AbnormalBillingValues = {
  align_with_official: false,
  continue_after_cloudflare_truncation: true,
  match_upstream_charge: false,
  charge_upstream_billed_failure: false,
  charge_policy_block: false,
}

const alignLabel = 'Align abnormal billing with official new-api'
const policyLabel = 'Charge requests blocked by an upstream safety policy'
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
  it('shows the five switches with the safety policy charge off and nothing to save', () => {
    render(<Fixture values={defaults} />)

    expect(screen.getAllByRole('switch')).toHaveLength(5)
    expect(screen.getByRole('switch', { name: alignLabel })).not.toBeChecked()
    expect(screen.getByRole('switch', { name: policyLabel })).not.toBeChecked()
    expect(
      screen.queryByRole('switch', {
        name: 'Charge when the client disconnects before upstream responds',
      })
    ).toBeNull()
    expect(
      screen.getByRole('button', { name: 'Save abnormal billing settings' })
    ).toBeDisabled()
  })

  it('saves only the toggled safety policy switch and returns to a clean state', async () => {
    const put = vi
      .spyOn(api, 'put')
      .mockResolvedValue({ data: { success: true } })
    const user = userEvent.setup()
    render(<Fixture values={defaults} />)

    await user.click(screen.getByRole('switch', { name: policyLabel }))
    const save = screen.getByRole('button', {
      name: 'Save abnormal billing settings',
    })
    expect(save).toBeEnabled()
    await user.click(save)

    await waitFor(() => expect(save).toBeDisabled())
    expect(put).toHaveBeenCalledTimes(1)
    expect(put).toHaveBeenCalledWith('/api/option/', {
      key: 'abnormal_billing_setting.charge_policy_block',
      value: 'true',
    })
    expect(screen.getByRole('switch', { name: policyLabel })).toBeChecked()
  })

  it('keeps a rejected switch dirty instead of reporting it as saved', async () => {
    vi.spyOn(api, 'put').mockResolvedValue({
      data: { success: false, message: 'must be true or false' },
    })
    const user = userEvent.setup()
    render(<Fixture values={defaults} />)

    await user.click(screen.getByRole('switch', { name: policyLabel }))
    const save = screen.getByRole('button', {
      name: 'Save abnormal billing settings',
    })
    await user.click(save)

    await waitFor(() => expect(save).toBeEnabled())
    expect(screen.getByRole('switch', { name: policyLabel })).toBeChecked()
  })

  it('keeps every switch usable while the official alignment is on', () => {
    render(<Fixture values={{ ...defaults, align_with_official: true }} />)

    expect(screen.getByRole('switch', { name: alignLabel })).toBeChecked()
    for (const item of screen.getAllByRole('switch')) {
      expect(item).not.toHaveAttribute('aria-disabled', 'true')
    }
  })

  it('keeps the Cloudflare switch usable and saves it alone while the official alignment is on', async () => {
    const put = vi
      .spyOn(api, 'put')
      .mockResolvedValue({ data: { success: true } })
    const user = userEvent.setup()
    render(<Fixture values={{ ...defaults, align_with_official: true }} />)

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
    render(<Fixture values={{ ...defaults, align_with_official: true }} />)

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

  it('saves the upstream-billed failure switch alone, off by default and usable under the official alignment', async () => {
    const put = vi
      .spyOn(api, 'put')
      .mockResolvedValue({ data: { success: true } })
    const user = userEvent.setup()
    render(<Fixture values={{ ...defaults, align_with_official: true }} />)

    const failure = screen.getByRole('switch', {
      name: 'Charge what upstream billed for a request it broke off',
    })
    expect(failure).not.toBeChecked()
    expect(failure).not.toHaveAttribute('aria-disabled', 'true')
    await user.click(failure)
    const save = screen.getByRole('button', {
      name: 'Save abnormal billing settings',
    })
    await user.click(save)

    await waitFor(() => expect(save).toBeDisabled())
    expect(put).toHaveBeenCalledTimes(1)
    expect(put).toHaveBeenCalledWith('/api/option/', {
      key: 'abnormal_billing_setting.charge_upstream_billed_failure',
      value: 'true',
    })
  })

  it('saves the official alignment alone', async () => {
    const put = vi
      .spyOn(api, 'put')
      .mockResolvedValue({ data: { success: true } })
    const user = userEvent.setup()
    render(<Fixture values={defaults} />)

    await user.click(screen.getByRole('switch', { name: alignLabel }))
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
    render(<Fixture values={defaults} />)

    await user.click(screen.getByRole('switch', { name: cloudflareLabel }))
    await user.click(screen.getByRole('switch', { name: policyLabel }))
    const save = screen.getByRole('button', {
      name: 'Save abnormal billing settings',
    })
    await user.click(save)

    await waitFor(() => expect(put).toHaveBeenCalledTimes(2))
    await waitFor(() => expect(save).toBeEnabled())
    // Undoing only the rejected switch leaves nothing to save, which holds only
    // if the accepted switch became the new saved baseline.
    await user.click(screen.getByRole('switch', { name: policyLabel }))
    expect(
      screen.getByRole('switch', { name: cloudflareLabel })
    ).not.toBeChecked()
    expect(save).toBeDisabled()
  })
})
