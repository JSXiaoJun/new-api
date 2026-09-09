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
import { AxiosHeaders } from 'axios'
import { toast } from 'sonner'
import { expect, test, vi } from 'vitest'

import { api } from '@/lib/api'

import { getUserQuotaLogs } from '../api'

test('quota history defaults to credit records and requests legacy logs only explicitly', async () => {
  const get = vi
    .spyOn(api, 'get')
    .mockResolvedValue({ data: { success: true } })
  try {
    await getUserQuotaLogs(42, { p: 2, page_size: 20 })
    expect(get).toHaveBeenLastCalledWith(
      '/api/user/42/quota/log',
      expect.objectContaining({
        params: { p: 2, page_size: 20, view: 'credits' },
      })
    )
    await getUserQuotaLogs(42, { p: 1, page_size: 10, view: 'legacy' })
    expect(get).toHaveBeenLastCalledWith(
      '/api/user/42/quota/log',
      expect.objectContaining({
        params: { p: 1, page_size: 10, view: 'legacy' },
      })
    )
  } finally {
    get.mockRestore()
  }
})

test('reopening quota history does not reuse a canceled request or show a cancellation error', async () => {
  const originalAdapter = api.defaults.adapter
  const toastError = vi.spyOn(toast, 'error')
  api.defaults.adapter = async (config) => ({
    config,
    data: {
      success: true,
      data: { items: [], total: 0, page: 1, page_size: 10 },
    },
    headers: new AxiosHeaders(),
    status: 200,
    statusText: 'OK',
  })
  try {
    const firstController = new AbortController()
    const firstRequest = getUserQuotaLogs(
      42,
      { p: 1, page_size: 10 },
      firstController.signal
    )
    const canceled = expect(firstRequest).rejects.toMatchObject({
      code: 'ERR_CANCELED',
    })
    firstController.abort()
    const reopened = getUserQuotaLogs(
      42,
      { p: 1, page_size: 10 },
      new AbortController().signal
    )

    await canceled
    await expect(reopened).resolves.toMatchObject({
      success: true,
      data: { items: [], total: 0 },
    })
    expect(toastError).not.toHaveBeenCalled()
  } finally {
    api.defaults.adapter = originalAdapter
  }
})
