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
import { act, render, screen, waitFor, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import type { ReactNode } from 'react'
import { afterEach, beforeEach, describe, expect, test, vi } from 'vitest'

import { TooltipProvider } from '@/components/ui/tooltip'
import { api } from '@/lib/api'

import type {
  ApiResponse,
  UserQuotaCredit,
  UserQuotaCreditsPage,
  UserQuotaLog,
} from '../../../types'
import { UserQuotaHistoryDialog } from '../user-quota-history-dialog'

const targetUser = { id: 282, username: 'quota-owner' }
const creditRecord: UserQuotaCredit = {
  id: 18,
  user_id: targetUser.id,
  created_at: 1787124215,
  delta: 25000000,
  source: 'admin_add',
  reference: 'admin-adjustment-282',
  request_id: 'request-282',
  operator_id: 7,
}
const legacyRecord: UserQuotaLog = {
  id: 17,
  user_id: targetUser.id,
  username: targetUser.username,
  created_at: 1787124215,
  type: 1,
  content: 'Online recharge succeeded: $25.00, order TOPUP-282',
  quota: 0,
  other: '',
}

function creditResponse(
  items: UserQuotaCredit[] = [creditRecord],
  total = items.length,
  page = 1,
  pageSize = 10
): { data: ApiResponse<UserQuotaCreditsPage> } {
  return {
    data: { success: true, data: { items, total, page, page_size: pageSize } },
  }
}

describe('User quota history dialog', () => {
  let queryClient: QueryClient
  function Wrapper(props: { children: ReactNode }) {
    return (
      <QueryClientProvider client={queryClient}>
        <TooltipProvider>{props.children}</TooltipProvider>
      </QueryClientProvider>
    )
  }
  beforeEach(() => {
    queryClient = new QueryClient({
      defaultOptions: { queries: { retry: false } },
    })
    vi.spyOn(api, 'get').mockResolvedValue(creditResponse())
  })
  afterEach(() => {
    queryClient.clear()
    vi.restoreAllMocks()
  })

  test('opening requests only the selected user and displays exact wallet credit with audit identifiers', async () => {
    const view = render(
      <UserQuotaHistoryDialog
        open={false}
        onOpenChange={vi.fn()}
        user={targetUser}
      />,
      { wrapper: Wrapper }
    )
    expect(api.get).not.toHaveBeenCalled()
    expect(screen.queryByRole('dialog')).not.toBeInTheDocument()
    view.rerender(
      <UserQuotaHistoryDialog open onOpenChange={vi.fn()} user={targetUser} />
    )
    expect(await screen.findByRole('cell', { name: '+25000000' })).toBeVisible()
    expect(screen.getByRole('tab', { name: 'Credit records' })).toHaveAttribute(
      'aria-selected',
      'true'
    )
    expect(
      screen.getByRole('cell', { name: 'Administrator credit' })
    ).toBeVisible()
    expect(screen.getByText(creditRecord.reference)).toBeVisible()
    expect(screen.getByText(creditRecord.request_id)).toBeVisible()
    expect(screen.getByRole('cell', { name: '7' })).toBeVisible()
    expect(api.get).toHaveBeenCalledWith(
      '/api/user/282/quota/log',
      expect.objectContaining({
        params: { p: 1, page_size: 10, view: 'credits' },
        signal: expect.any(AbortSignal),
      })
    )
    expect(
      screen.getByRole('dialog', { name: 'Quota History' })
    ).toHaveAccessibleDescription('quota-owner (User ID: 282)')
    expect(
      screen.getByRole('button', { name: 'Go to previous page' })
    ).toBeDisabled()
    expect(
      screen.getByRole('button', { name: 'Go to next page' })
    ).toBeDisabled()
    expect(
      screen.getByText(
        'Wallet credit records begin after auditing is enabled. Unrecorded or deleted history cannot be recovered.'
      )
    ).toBeVisible()
  })

  test('pagination keeps the user and credit view while a new page size restarts at page one', async () => {
    vi.mocked(api.get).mockResolvedValue(creditResponse([creditRecord], 25))
    const user = userEvent.setup()
    render(
      <UserQuotaHistoryDialog open onOpenChange={vi.fn()} user={targetUser} />,
      { wrapper: Wrapper }
    )
    await screen.findByText(creditRecord.reference)
    await user.click(screen.getByRole('button', { name: 'Go to next page' }))
    await waitFor(() =>
      expect(api.get).toHaveBeenLastCalledWith(
        '/api/user/282/quota/log',
        expect.objectContaining({
          params: { p: 2, page_size: 10, view: 'credits' },
        })
      )
    )
    await waitFor(() =>
      expect(
        screen.getByRole('button', { name: 'Go to previous page' })
      ).toBeEnabled()
    )
    await user.click(
      screen.getByRole('button', { name: 'Go to previous page' })
    )
    await waitFor(() =>
      expect(api.get).toHaveBeenLastCalledWith(
        '/api/user/282/quota/log',
        expect.objectContaining({
          params: { p: 1, page_size: 10, view: 'credits' },
        })
      )
    )
    await waitFor(() =>
      expect(
        screen.getByRole('button', { name: 'Go to next page' })
      ).toBeEnabled()
    )
    await user.click(screen.getByRole('button', { name: 'Go to next page' }))
    await waitFor(() =>
      expect(
        screen.getByRole('combobox', { name: 'Rows per page' })
      ).toBeEnabled()
    )
    await user.selectOptions(
      screen.getByRole('combobox', { name: 'Rows per page' }),
      '20'
    )
    await waitFor(() =>
      expect(api.get).toHaveBeenLastCalledWith(
        '/api/user/282/quota/log',
        expect.objectContaining({
          params: { p: 1, page_size: 20, view: 'credits' },
        })
      )
    )
    expect(screen.getByText('Page 1 of 2')).toBeVisible()
    expect(
      screen.getByRole('button', { name: 'Go to previous page' })
    ).toBeDisabled()
  })

  test('keyboard tab switching resets pagination without mixing credit records with historical logs', async () => {
    vi.mocked(api.get).mockResolvedValue(creditResponse([creditRecord], 25))
    const user = userEvent.setup()
    render(
      <UserQuotaHistoryDialog open onOpenChange={vi.fn()} user={targetUser} />,
      { wrapper: Wrapper }
    )
    await screen.findByText(creditRecord.reference)
    await user.click(screen.getByRole('button', { name: 'Go to next page' }))
    await waitFor(() =>
      expect(screen.getByRole('button', { name: 'Refresh' })).toBeEnabled()
    )
    let complete!: (response: unknown) => void
    vi.mocked(api.get).mockImplementationOnce(
      () =>
        new Promise((resolve) => {
          complete = resolve
        })
    )
    screen.getByRole('tab', { name: 'Credit records' }).focus()
    await user.keyboard('{ArrowRight}{Enter}')
    expect(
      screen.getByRole('tab', { name: 'Historical logs' })
    ).toHaveAttribute('aria-selected', 'true')
    expect(screen.queryByText(creditRecord.reference)).not.toBeInTheDocument()
    expect(screen.getByRole('status', { name: 'Loading...' })).toBeVisible()
    expect(api.get).toHaveBeenLastCalledWith(
      '/api/user/282/quota/log',
      expect.objectContaining({
        params: { p: 1, page_size: 10, view: 'legacy' },
      })
    )
    await act(async () =>
      complete({
        data: {
          success: true,
          data: { items: [legacyRecord], total: 1, page: 1, page_size: 10 },
        },
      })
    )
    expect(
      await screen.findByRole('cell', { name: legacyRecord.content })
    ).toBeVisible()
    expect(screen.getByRole('cell', { name: 'Unknown amount' })).toBeVisible()
    expect(
      screen.getByRole('cell', { name: 'Recharge / subscription' })
    ).toBeVisible()
    expect(
      screen.getByText(
        'Historical logs may not identify the amount or balance account. They do not prove wallet credit.'
      )
    ).toBeVisible()
    await user.click(screen.getByRole('tab', { name: 'Credit records' }))
    expect(await screen.findByText(creditRecord.reference)).toBeVisible()
    expect(screen.queryByText(legacyRecord.content)).not.toBeInTheDocument()
    expect(screen.getByText('Page 1 of 3')).toBeVisible()
  })

  test.each([
    ['user.quota_add', { quota: '$10.00' }, 'Increased user quota by $10.00'],
    [
      'user.quota_subtract',
      { quota: '$3.00' },
      'Decreased user quota by $3.00',
    ],
    [
      'user.quota_override',
      { from: '$4.00', to: '$12.00' },
      'Overrode user quota from $4.00 to $12.00',
    ],
  ])(
    'historical %s retains the recorded amounts and administrator identity',
    async (action, params, content) => {
      const user = userEvent.setup()
      render(
        <UserQuotaHistoryDialog
          open
          onOpenChange={vi.fn()}
          user={targetUser}
        />,
        { wrapper: Wrapper }
      )
      await screen.findByText(creditRecord.reference)
      vi.mocked(api.get).mockResolvedValueOnce({
        data: {
          success: true,
          data: {
            items: [
              {
                ...legacyRecord,
                type: 3,
                content: '',
                other: JSON.stringify({
                  op: { action, params },
                  admin_info: { admin_id: 1, admin_username: 'quota-admin' },
                }),
              },
            ],
            total: 1,
            page: 1,
            page_size: 10,
          },
        },
      })
      await user.click(screen.getByRole('tab', { name: 'Historical logs' }))
      const row = await screen.findByRole('row', {
        name: new RegExp(content.replaceAll('$', '\\$')),
      })
      expect(within(row).getByRole('cell', { name: content })).toBeVisible()
      expect(
        within(row).getByRole('cell', { name: 'quota-admin' })
      ).toBeVisible()
      expect(within(row).getByRole('cell', { name: 'Manage' })).toBeVisible()
    }
  )

  test('historical negative consumption is flagged and keeps its signed raw quota', async () => {
    const user = userEvent.setup()
    render(
      <UserQuotaHistoryDialog open onOpenChange={vi.fn()} user={targetUser} />,
      { wrapper: Wrapper }
    )
    await screen.findByText(creditRecord.reference)
    vi.mocked(api.get).mockResolvedValueOnce({
      data: {
        success: true,
        data: {
          items: [
            {
              ...legacyRecord,
              type: 2,
              quota: -750000,
              content: 'Negative billing result',
            },
          ],
          total: 1,
          page: 1,
          page_size: 10,
        },
      },
    })
    await user.click(screen.getByRole('tab', { name: 'Historical logs' }))
    expect(
      await screen.findByRole('cell', {
        name: 'Negative consumption (anomaly)',
      })
    ).toBeVisible()
    expect(screen.getByText('Negative consumption (anomaly)')).toHaveClass(
      'h-auto',
      'whitespace-normal'
    )
    expect(screen.getByRole('cell', { name: '-750000' })).toBeVisible()
  })

  test('empty credits show an empty state and disable paging', async () => {
    vi.mocked(api.get).mockResolvedValue(creditResponse([]))
    render(
      <UserQuotaHistoryDialog open onOpenChange={vi.fn()} user={targetUser} />,
      { wrapper: Wrapper }
    )
    expect(await screen.findByText('No quota records found')).toBeVisible()
    expect(screen.getByText('Page 1 of 1')).toBeVisible()
    expect(
      screen.getByRole('button', { name: 'Go to previous page' })
    ).toBeDisabled()
    expect(
      screen.getByRole('button', { name: 'Go to next page' })
    ).toBeDisabled()
  })

  test.each(['network', 'unsuccessful response'])(
    '%s failure displays an error and retry recovers credits',
    async (failure) => {
      if (failure === 'network') {
        vi.mocked(api.get).mockRejectedValueOnce(
          new Error('Network unavailable')
        )
      } else {
        vi.mocked(api.get).mockResolvedValueOnce({
          data: { success: false, message: 'Database unavailable' },
        })
      }
      const user = userEvent.setup()
      render(
        <UserQuotaHistoryDialog
          open
          onOpenChange={vi.fn()}
          user={targetUser}
        />,
        { wrapper: Wrapper }
      )
      expect(await screen.findByRole('alert')).toHaveTextContent(
        'Failed to load quota records'
      )
      expect(
        screen.queryByText('No quota records found')
      ).not.toBeInTheDocument()
      expect(
        screen.getByRole('button', { name: 'Go to next page' })
      ).toBeDisabled()
      await user.click(screen.getByRole('button', { name: 'Retry' }))
      expect(await screen.findByText(creditRecord.reference)).toBeVisible()
      expect(screen.queryByRole('alert')).not.toBeInTheDocument()
    }
  )

  test('pending requests disable paging, page size, and refresh until credits arrive', async () => {
    let complete!: (response: ReturnType<typeof creditResponse>) => void
    vi.mocked(api.get).mockImplementationOnce(
      () =>
        new Promise((resolve) => {
          complete = resolve
        })
    )
    render(
      <UserQuotaHistoryDialog open onOpenChange={vi.fn()} user={targetUser} />,
      { wrapper: Wrapper }
    )
    expect(screen.getByRole('status', { name: 'Loading...' })).toBeVisible()
    expect(
      screen.getByRole('button', { name: 'Go to next page' })
    ).toBeDisabled()
    expect(
      screen.getByRole('combobox', { name: 'Rows per page' })
    ).toBeDisabled()
    expect(screen.getByRole('button', { name: 'Refresh' })).toBeDisabled()
    await act(async () => complete(creditResponse([creditRecord], 15)))
    expect(await screen.findByText(creditRecord.reference)).toBeVisible()
    expect(
      screen.queryByRole('status', { name: 'Loading...' })
    ).not.toBeInTheDocument()
    expect(
      screen.getByRole('button', { name: 'Go to next page' })
    ).toBeEnabled()
    expect(
      screen.getByRole('combobox', { name: 'Rows per page' })
    ).toBeEnabled()
    expect(screen.getByRole('button', { name: 'Refresh' })).toBeEnabled()
  })

  test('refresh retains the current page and marks retained credits busy until it completes', async () => {
    vi.mocked(api.get).mockResolvedValue(creditResponse([creditRecord], 15))
    const user = userEvent.setup()
    render(
      <UserQuotaHistoryDialog open onOpenChange={vi.fn()} user={targetUser} />,
      { wrapper: Wrapper }
    )
    await screen.findByText(creditRecord.reference)
    await user.click(screen.getByRole('button', { name: 'Go to next page' }))
    await waitFor(() =>
      expect(screen.getByRole('button', { name: 'Refresh' })).toBeEnabled()
    )
    let complete!: (response: ReturnType<typeof creditResponse>) => void
    vi.mocked(api.get).mockImplementationOnce(
      () =>
        new Promise((resolve) => {
          complete = resolve
        })
    )
    await user.click(screen.getByRole('button', { name: 'Refresh' }))
    expect(api.get).toHaveBeenLastCalledWith(
      '/api/user/282/quota/log',
      expect.objectContaining({
        params: { p: 2, page_size: 10, view: 'credits' },
      })
    )
    expect(
      screen.getByRole('table', { name: 'Credit records' })
    ).toHaveAttribute('aria-busy', 'true')
    expect(screen.getByText(creditRecord.reference)).toBeVisible()
    expect(screen.getByRole('button', { name: 'Refresh' })).toBeDisabled()
    expect(
      screen.getByRole('button', { name: 'Go to previous page' })
    ).toBeDisabled()
    await act(async () =>
      complete(creditResponse([{ ...creditRecord, delta: 30000000 }], 15, 2))
    )
    expect(await screen.findByRole('cell', { name: '+30000000' })).toBeVisible()
    expect(
      screen.getByRole('table', { name: 'Credit records' })
    ).toHaveAttribute('aria-busy', 'false')
    expect(screen.getByText('Page 2 of 2')).toBeVisible()
  })

  test('changing users removes previous credits and resets to the first credit page', async () => {
    const view = render(
      <UserQuotaHistoryDialog
        key={282}
        open
        onOpenChange={vi.fn()}
        user={targetUser}
      />,
      { wrapper: Wrapper }
    )
    await screen.findByText(creditRecord.reference)
    let complete!: (response: ReturnType<typeof creditResponse>) => void
    vi.mocked(api.get).mockImplementationOnce(
      () =>
        new Promise((resolve) => {
          complete = resolve
        })
    )
    view.rerender(
      <UserQuotaHistoryDialog
        key={303}
        open
        onOpenChange={vi.fn()}
        user={{ id: 303, username: 'second-owner' }}
      />
    )
    expect(screen.queryByText(creditRecord.reference)).not.toBeInTheDocument()
    expect(screen.getByRole('status', { name: 'Loading...' })).toBeVisible()
    expect(api.get).toHaveBeenLastCalledWith(
      '/api/user/303/quota/log',
      expect.objectContaining({
        params: { p: 1, page_size: 10, view: 'credits' },
      })
    )
    await act(async () =>
      complete(
        creditResponse([
          { ...creditRecord, user_id: 303, reference: 'second-user-credit' },
        ])
      )
    )
    expect(await screen.findByText('second-user-credit')).toBeVisible()
    expect(screen.queryByText(creditRecord.reference)).not.toBeInTheDocument()
  })

  test('long references, unknown sources, and usernames remain complete with wrapping', async () => {
    const longName = `quota-owner-${'a'.repeat(150)}`
    const longReference = `order-${'reference'.repeat(50)}`
    const unknownSource = `unrecognized-${'source'.repeat(30)}`
    vi.mocked(api.get).mockResolvedValue(
      creditResponse([
        {
          ...creditRecord,
          reference: longReference,
          request_id: '',
          source: unknownSource,
        },
      ])
    )
    render(
      <UserQuotaHistoryDialog
        open
        onOpenChange={vi.fn()}
        user={{ ...targetUser, username: longName }}
      />,
      { wrapper: Wrapper }
    )
    const reference = await screen.findByRole('cell', { name: longReference })
    expect(reference).toBeVisible()
    expect(reference).toHaveClass('whitespace-normal', 'break-all')
    expect(screen.getByRole('cell', { name: unknownSource })).toHaveClass(
      'whitespace-normal',
      '[overflow-wrap:anywhere]'
    )
    expect(screen.getByText(`${longName} (User ID: 282)`)).toHaveClass(
      'break-all'
    )
  })
})
