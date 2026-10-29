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
import { api } from '@/lib/api'
import { readExportStream } from '@/lib/csv'

import type {
  ExportTopUpOrdersParams,
  GetTopUpOrdersParams,
  GetTopUpOrdersResponse,
  TopUpOrder,
} from './types'

export async function getTopUpOrders(
  params: GetTopUpOrdersParams = {}
): Promise<GetTopUpOrdersResponse> {
  const response = await api.get('/api/user/topup', {
    params: {
      p: params.p ?? 1,
      page_size: params.page_size ?? 20,
      keyword: params.keyword?.trim() || undefined,
      user_id: params.user_id,
    },
  })
  return response.data
}

/**
 * Stream every recharge order matching params, newest first. Resolves with
 * the order count.
 */
export function exportTopUpOrders(
  params: ExportTopUpOrdersParams,
  options: { signal: AbortSignal; onOrder: (order: TopUpOrder) => void }
): Promise<number> {
  const query = new URLSearchParams()
  const keyword = params.keyword?.trim()
  if (keyword) query.set('keyword', keyword)
  if (params.user_id !== undefined) query.set('user_id', String(params.user_id))
  if (params.start_timestamp !== undefined) {
    query.set('start_timestamp', String(params.start_timestamp))
  }
  if (params.end_timestamp !== undefined) {
    query.set('end_timestamp', String(params.end_timestamp))
  }
  return readExportStream<TopUpOrder>(`/api/user/topup/export?${query}`, {
    signal: options.signal,
    onRecord: options.onOrder,
  })
}
