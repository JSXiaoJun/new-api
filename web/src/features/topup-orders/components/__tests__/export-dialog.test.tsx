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
import {
  cleanup,
  render,
  screen,
  waitFor,
  within,
} from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { toast } from 'sonner'
import { afterEach, beforeEach, expect, it, vi } from 'vitest'

import { ROLE } from '@/lib/roles'
import { useAuthStore } from '@/stores/auth-store'

import { ExportTopUpOrdersDialog } from '../export-topup-orders-dialog'

const createObjectURL = vi.fn((_blob: Blob) => 'blob:orders')

beforeEach(() => {
  useAuthStore.setState((state) => ({
    auth: {
      ...state.auth,
      user: { id: 1, username: 'admin', role: ROLE.ADMIN },
      accessToken: 'test-token',
      accessExpiresAt: Math.floor(Date.now() / 1000) + 3600,
    },
  }))
  Object.defineProperty(URL, 'createObjectURL', {
    configurable: true,
    value: createObjectURL,
  })
  Object.defineProperty(URL, 'revokeObjectURL', {
    configurable: true,
    value: vi.fn(),
  })
  vi.spyOn(HTMLAnchorElement.prototype, 'click').mockImplementation(() => {})
})

afterEach(() => {
  cleanup()
  createObjectURL.mockClear()
  vi.restoreAllMocks()
  Reflect.deleteProperty(URL, 'createObjectURL')
  Reflect.deleteProperty(URL, 'revokeObjectURL')
  useAuthStore.getState().auth.reset()
})

function mockExportResponse(lines: unknown[]) {
  return vi
    .spyOn(globalThis, 'fetch')
    .mockResolvedValue(
      new Response(lines.map((line) => JSON.stringify(line)).join('\n'))
    )
}

const order = {
  id: 1,
  user_id: 282,
  username: 'order-owner',
  display_name: '',
  email: '',
  amount: 20,
  money: 20,
  trade_no: 'MATCH-order',
  payment_method: 'alipay',
  payment_provider: 'epay',
  create_time: 1793278008,
  complete_time: 0,
  status: 'pending',
}

async function startExport() {
  const user = userEvent.setup()
  await user.click(screen.getByRole('button', { name: 'Export' }))
  await user.click(
    within(screen.getByRole('dialog')).getByRole('button', { name: 'Export' })
  )
}

it('downloads the orders matching the page filters as CSV', async () => {
  const fetchSpy = mockExportResponse([
    order,
    { success: true, message: '', data: { count: 1 } },
  ])
  const success = vi.spyOn(toast, 'success')
  render(<ExportTopUpOrdersDialog keyword=' MATCH ' userId={282} />)

  await startExport()

  await waitFor(() => expect(createObjectURL).toHaveBeenCalledTimes(1))
  const [url] = fetchSpy.mock.calls[0]
  expect(String(url)).toMatch(/^\/api\/user\/topup\/export\?/)
  const params = new URL(String(url), 'http://localhost').searchParams
  expect(params.get('keyword')).toBe('MATCH')
  expect(params.get('user_id')).toBe('282')
  expect(Number(params.get('end_timestamp'))).toBeGreaterThan(
    Number(params.get('start_timestamp'))
  )
  const file = createObjectURL.mock.calls[0][0]
  const bytes = new Uint8Array(await file.arrayBuffer())
  expect(bytes.subarray(0, 3)).toEqual(new Uint8Array([0xef, 0xbb, 0xbf]))
  const rows = (await file.text()).trimEnd().split('\r\n')
  expect(rows).toHaveLength(2)
  expect(rows[1]).toContain('"MATCH-order","order-owner (ID: 282)"')
  expect(success).toHaveBeenCalledWith('Exported 1 orders')
  await waitFor(() => expect(screen.queryByRole('dialog')).toBeNull())
})

it('reports a server refusal and keeps the dialog open without a download', async () => {
  mockExportResponse([
    {
      success: false,
      message: 'Fuzzy search keywords must be at least 2 characters long',
      message_key: 'Fuzzy search keywords must be at least 2 characters long',
    },
  ])
  const error = vi.spyOn(toast, 'error')
  render(<ExportTopUpOrdersDialog keyword='%M%' />)

  await startExport()

  await waitFor(() =>
    expect(error).toHaveBeenCalledWith(
      'Fuzzy search keywords must be at least 2 characters long'
    )
  )
  expect(createObjectURL).not.toHaveBeenCalled()
  expect(screen.getByRole('dialog')).toBeVisible()
})

it('treats a stream that ends without the success line as a failed export', async () => {
  mockExportResponse([order])
  const error = vi.spyOn(toast, 'error')
  render(<ExportTopUpOrdersDialog keyword='' />)

  await startExport()

  await waitFor(() =>
    expect(error).toHaveBeenCalledWith('Failed to export recharge orders')
  )
  expect(createObjectURL).not.toHaveBeenCalled()
})

it('disables Export while the page filters are invalid', () => {
  const fetchSpy = vi.spyOn(globalThis, 'fetch')
  render(<ExportTopUpOrdersDialog keyword='' disabled />)

  expect(screen.getByRole('button', { name: 'Export' })).toBeDisabled()
  expect(fetchSpy).not.toHaveBeenCalled()
})
