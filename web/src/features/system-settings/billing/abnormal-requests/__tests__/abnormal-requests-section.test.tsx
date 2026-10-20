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
import { render, screen, waitFor, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { createInstance, type i18n } from 'i18next'
import { useState } from 'react'
import { I18nextProvider } from 'react-i18next'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'

import { api } from '@/lib/api'

import { SettingsPageProvider } from '../../../components/settings-page-context'
import { AbnormalRequestsSection } from '../abnormal-requests-section'
import type { AbnormalChargeRecord, AbnormalChargeUser } from '../types'

const suspicious: AbnormalChargeUser = {
  user_id: 42,
  username: 'abuser',
  count: 12,
  received_count: 3,
  forgone_quota: 90_000,
  pending_quota: 80_000,
  pending_count: 10,
  max_pending_id: 99,
  upstream_billed_count: 2,
  last_at: 1_790_000_000,
  avg_elapsed_ms: 20_000,
  elapsed_stddev_ms: 100,
  peak_hour_count: 12,
  peak_hour_weighted: 15,
  peak_hour_requests: 14,
  peak_hour_ratio_known: true,
  peak_hour_ratio_percent: 85.7,
  fixed_timing: true,
  flagged: true,
  flag_reasons: ['hourly_count', 'forgone_quota'],
  strict_billing: false,
}

function record(id: number, overrides: Partial<AbnormalChargeRecord> = {}) {
  return {
    id,
    user_id: 42,
    username: 'abuser',
    created_at: 1_790_000_000,
    request_id: `req-${id}`,
    token_name: 'key',
    model_name: 'gpt-test',
    channel_id: 1,
    ip: '203.0.113.7',
    is_stream: true,
    is_task: false,
    case: 'client_during_response',
    user_caused: true,
    ended_by: 'client',
    phase: 'receiving',
    reason: 'client_gone',
    elapsed_ms: 20_000,
    client_received_data: true,
    outcome: 'waived',
    forgone_quota: 5_000,
    upstream_billing: '',
    upstream_quota: 0,
    upstream_prompt_tokens: 0,
    upstream_completion_tokens: 0,
    charged_quota: 0,
    recovery_id: '',
    recovered_at: 0,
    ...overrides,
  } satisfies AbnormalChargeRecord
}

let testI18n: i18n

function Fixture() {
  const [container, setContainer] = useState<HTMLDivElement | null>(null)
  const [client] = useState(
    () =>
      new QueryClient({
        defaultOptions: {
          queries: { retry: false },
          mutations: { retry: false },
        },
      })
  )
  return (
    <I18nextProvider i18n={testI18n}>
      <QueryClientProvider client={client}>
        <div ref={setContainer} />
        <SettingsPageProvider actionsContainer={container}>
          <AbnormalRequestsSection
            defaultValues={{
              abnormal_request_setting: {
                flag_hourly_count: 10,
                flag_ratio_percent: 80,
                flag_min_requests: 20,
                flag_forgone_quota: 68_500,
              },
            }}
          />
        </SettingsPageProvider>
      </QueryClientProvider>
    </I18nextProvider>
  )
}

function page<T>(items: T[], total = items.length) {
  return {
    data: {
      success: true,
      data: { page: 1, page_size: 20, total, items },
    },
  }
}

type ListOptions = {
  records?: AbnormalChargeRecord[]
  recordTotal?: number
  users?: AbnormalChargeUser[]
  userTotal?: number
}

function mockLists(options: ListOptions = {}) {
  return vi
    .spyOn(api, 'get')
    .mockImplementation(async (url: string): Promise<unknown> => {
      if (url === '/api/user/abnormal_charges') {
        return page(options.users ?? [suspicious], options.userTotal)
      }
      if (url === '/api/user/abnormal_charges/records') {
        return page(options.records ?? [], options.recordTotal)
      }
      throw new Error(`unexpected GET ${url}`)
    })
}

function lastParams(get: ReturnType<typeof mockLists>, url: string) {
  const calls = get.mock.calls.filter((call) => call[0] === url)
  const config = calls.at(-1)?.[1] as
    | { params: Record<string, unknown> }
    | undefined
  if (!config) throw new Error(`no GET ${url}`)
  return config.params
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

describe('abnormal requests section', () => {
  it('lists a flagged user with the reasons it was flagged', async () => {
    mockLists()
    render(<Fixture />)

    const row = (await screen.findByText('abuser')).closest('tr')
    expect(row).not.toBeNull()
    const cells = within(row as HTMLElement)
    expect(cells.getByText('Suspicious')).toBeInTheDocument()
    expect(cells.getByText('Many in one hour')).toBeInTheDocument()
    expect(cells.getByText('Large amount')).toBeInTheDocument()
    expect(cells.getByText('Fixed timing')).toBeInTheDocument()
    expect(cells.getByText('Upstream charged 2')).toBeInTheDocument()
    expect(
      cells.getByRole('switch', { name: 'Strict billing for abuser' })
    ).not.toBeChecked()
  })

  it('recovers only the pending charges the admin reviewed, after confirmation', async () => {
    mockLists()
    const post = vi.spyOn(api, 'post').mockResolvedValue({
      data: {
        success: true,
        data: {
          recovered_records: 10,
          quota: 80_000,
          from: 1_000,
          to: -79_000,
        },
      },
    })
    const user = userEvent.setup()
    render(<Fixture />)

    await user.click(await screen.findByRole('button', { name: 'Recover' }))
    expect(post).not.toHaveBeenCalled()
    const dialog = await screen.findByRole('alertdialog')
    await user.click(within(dialog).getByRole('button', { name: 'Recover' }))

    await waitFor(() => expect(post).toHaveBeenCalledTimes(1))
    expect(post).toHaveBeenCalledWith(
      '/api/user/42/abnormal_charges/recover',
      { ids: undefined, days: 1, max_id: 99 },
      expect.anything()
    )
  })

  it('opens the records of one user from the user list and recovers only the ticked ones', async () => {
    const get = mockLists({
      records: [
        record(1),
        record(2, {
          upstream_billing: 'billed',
          upstream_quota: 9_000,
          charged_quota: 4_000,
          forgone_quota: 5_000,
          outcome: 'partial',
        }),
        record(3, { user_caused: false, case: 'upstream_during_response' }),
      ],
    })
    const post = vi.spyOn(api, 'post').mockResolvedValue({
      data: {
        success: true,
        data: { recovered_records: 1, quota: 5_000, from: 0, to: -5_000 },
      },
    })
    const user = userEvent.setup()
    render(<Fixture />)

    await user.click(await screen.findByRole('button', { name: 'Records' }))
    const dialog = await screen.findByRole('tabpanel')
    await within(dialog).findByText('Not caused by the user')
    expect(lastParams(get, '/api/user/abnormal_charges/records')).toMatchObject(
      { user_id: 42, status: 'pending', p: 1 }
    )
    expect(within(dialog).getByText('Upstream charged')).toBeInTheDocument()
    expect(
      within(dialog).queryByRole('checkbox', { name: 'Select record 3' })
    ).not.toBeInTheDocument()
    const submit = within(dialog).getByRole('button', {
      name: /^Recover \d+ selected/,
    })
    expect(submit).toBeDisabled()

    await user.click(
      within(dialog).getByRole('checkbox', { name: 'Select record 2' })
    )
    expect(submit).toBeEnabled()
    await user.click(submit)
    const confirm = await screen.findByRole('alertdialog')
    await user.click(within(confirm).getByRole('button', { name: 'Recover' }))

    await waitFor(() => expect(post).toHaveBeenCalledTimes(1))
    expect(post).toHaveBeenCalledWith(
      '/api/user/42/abnormal_charges/recover',
      { ids: [2], days: 1, max_id: undefined },
      expect.anything()
    )
  })

  it('turns strict billing on for one user', async () => {
    mockLists()
    const put = vi
      .spyOn(api, 'put')
      .mockResolvedValue({ data: { success: true, data: { enabled: true } } })
    const user = userEvent.setup()
    render(<Fixture />)

    await user.click(
      await screen.findByRole('switch', { name: 'Strict billing for abuser' })
    )

    await waitFor(() => expect(put).toHaveBeenCalledTimes(1))
    expect(put).toHaveBeenCalledWith(
      '/api/user/42/strict_billing',
      { enabled: true },
      expect.anything()
    )
  })

  it('shows a failed recovery instead of reporting success', async () => {
    mockLists()
    vi.spyOn(api, 'post').mockResolvedValue({
      data: {
        success: false,
        message: 'No pending abnormal charges to recover',
        data: null,
      },
    })
    const user = userEvent.setup()
    render(<Fixture />)

    await user.click(await screen.findByRole('button', { name: 'Recover' }))
    const dialog = await screen.findByRole('alertdialog')
    await user.click(within(dialog).getByRole('button', { name: 'Recover' }))

    await waitFor(() =>
      expect(
        within(dialog).getByRole('button', { name: 'Recover' })
      ).toBeEnabled()
    )
    expect(screen.getByRole('alertdialog')).toBeInTheDocument()
  })

  it('lists the records of every user page by page and offers recovery only for one picked user', async () => {
    const get = mockLists({
      records: [record(7), record(8, { user_id: 51, username: 'other' })],
      recordTotal: 45,
    })
    const user = userEvent.setup()
    render(<Fixture />)

    await user.click(await screen.findByRole('tab', { name: 'All records' }))
    const panel = await screen.findByRole('tabpanel')
    await within(panel).findByText('other')
    const first = lastParams(get, '/api/user/abnormal_charges/records')
    expect(first).toMatchObject({ p: 1, page_size: 20 })
    expect(first.user_id).toBeUndefined()
    expect(within(panel).queryByRole('checkbox')).not.toBeInTheDocument()
    expect(
      within(panel).getByText('Pick one user to choose records to recover.')
    ).toBeInTheDocument()

    await user.click(
      within(panel).getByRole('button', { name: /^Go to page 3/ })
    )
    await waitFor(() =>
      expect(
        lastParams(get, '/api/user/abnormal_charges/records')
      ).toMatchObject({ p: 3 })
    )

    await user.click(
      within(panel).getByRole('button', {
        name: 'Show only the records of other',
      })
    )
    await waitFor(() =>
      expect(
        lastParams(get, '/api/user/abnormal_charges/records')
      ).toMatchObject({ user_id: 51, p: 1 })
    )
    expect(
      await within(panel).findByRole('button', {
        name: /^Recover \d+ selected/,
      })
    ).toBeDisabled()
  })

  it('searches records by a typed username without pinning a user', async () => {
    const get = mockLists({ records: [record(7)] })
    const user = userEvent.setup()
    render(<Fixture />)

    await user.click(await screen.findByRole('tab', { name: 'All records' }))
    const panel = await screen.findByRole('tabpanel')
    await user.type(
      within(panel).getByRole('combobox', { name: 'Filter by user' }),
      'abu'
    )

    await waitFor(() =>
      expect(
        lastParams(get, '/api/user/abnormal_charges/records')
      ).toMatchObject({ keyword: 'abu', p: 1 })
    )
    expect(
      lastParams(get, '/api/user/abnormal_charges/records').user_id
    ).toBeUndefined()
  })

  it('pages through the user list', async () => {
    const get = mockLists({ userTotal: 45 })
    const user = userEvent.setup()
    render(<Fixture />)

    await screen.findByText('abuser')
    await user.click(screen.getByRole('button', { name: 'Go to next page' }))

    await waitFor(() =>
      expect(lastParams(get, '/api/user/abnormal_charges')).toMatchObject({
        p: 2,
        page_size: 20,
      })
    )
  })
})
