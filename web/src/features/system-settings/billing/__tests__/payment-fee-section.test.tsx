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
  PaymentFeeSection,
  type PaymentFeeValues,
} from '../payment-fee-section'

const defaults: PaymentFeeValues = { enabled: false, threshold: 0, rate: 0 }

const switchLabel = 'Charge the fee to users below the threshold'
const saveLabel = 'Save payment fee settings'

let testI18n: i18n

function Fixture(props: { values: PaymentFeeValues }) {
  const [container, setContainer] = useState<HTMLDivElement | null>(null)
  const [client] = useState(
    () => new QueryClient({ defaultOptions: { mutations: { retry: false } } })
  )
  return (
    <I18nextProvider i18n={testI18n}>
      <QueryClientProvider client={client}>
        <div ref={setContainer} />
        <SettingsPageProvider actionsContainer={container}>
          <PaymentFeeSection defaultValues={props.values} />
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

describe('payment fee section', () => {
  it('saves the threshold and rate before turning the fee on', async () => {
    const put = vi
      .spyOn(api, 'put')
      .mockResolvedValue({ data: { success: true } })
    const user = userEvent.setup()
    render(<Fixture values={defaults} />)

    await user.click(screen.getByRole('switch', { name: switchLabel }))
    const threshold = screen.getByLabelText('Fee threshold (CNY)')
    await user.clear(threshold)
    await user.type(threshold, '50')
    const rate = screen.getByLabelText('Fee rate (%)')
    await user.clear(rate)
    await user.type(rate, '0.6')
    const save = screen.getByRole('button', { name: saveLabel })
    await user.click(save)

    await waitFor(() => expect(save).toBeDisabled())
    expect(put.mock.calls.map((call) => call[1])).toEqual([
      { key: 'payment_setting.epay_fee_threshold', value: '50' },
      { key: 'payment_setting.epay_fee_rate', value: '0.6' },
      { key: 'payment_setting.epay_fee_enabled', value: 'true' },
    ])
  })

  it('rejects a rate above the cap without saving anything', async () => {
    const put = vi.spyOn(api, 'put')
    const user = userEvent.setup()
    render(<Fixture values={defaults} />)

    const rate = screen.getByLabelText('Fee rate (%)')
    await user.clear(rate)
    await user.type(rate, '60')
    await user.click(screen.getByRole('button', { name: saveLabel }))

    await waitFor(() => expect(rate).toHaveAttribute('aria-invalid', 'true'))
    expect(put).not.toHaveBeenCalled()
  })
})
