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
import { requireServerSuccess } from '@/lib/server-error-message'

import type {
  AbnormalChargeRecord,
  AbnormalChargeRecordStatus,
  AbnormalChargeUser,
  AbnormalRecovery,
  ApiPage,
  ApiResult,
} from './types'

const queryConfig = {
  // React Query owns cancellation and deduplication for these requests.
  disableDuplicate: true,
  skipErrorHandler: true,
  skipBusinessError: true,
}

export async function getAbnormalChargeUsers(
  params: { days: number; keyword: string; p: number; page_size: number },
  signal?: AbortSignal
): Promise<ApiPage<AbnormalChargeUser>> {
  const res = await api.get<ApiResult<ApiPage<AbnormalChargeUser>>>(
    '/api/user/abnormal_charges',
    { params, signal, ...queryConfig }
  )
  return requireServerSuccess(res.data).data
}

export async function getUserAbnormalCharges(
  userId: number,
  params: {
    days: number
    status: AbnormalChargeRecordStatus
    p: number
    page_size: number
  },
  signal?: AbortSignal
): Promise<ApiPage<AbnormalChargeRecord>> {
  const res = await api.get<ApiResult<ApiPage<AbnormalChargeRecord>>>(
    `/api/user/${userId}/abnormal_charges`,
    { params, signal, ...queryConfig }
  )
  return requireServerSuccess(res.data).data
}

/** Recovers the given records, or every pending one within `days`. */
export async function recoverUserAbnormalCharges(
  userId: number,
  request: { ids?: number[]; days: number; max_id?: number }
): Promise<AbnormalRecovery> {
  const res = await api.post<ApiResult<AbnormalRecovery>>(
    `/api/user/${userId}/abnormal_charges/recover`,
    request,
    { skipErrorHandler: true, skipBusinessError: true }
  )
  return requireServerSuccess(res.data).data
}

export async function setUserStrictBilling(
  userId: number,
  enabled: boolean
): Promise<void> {
  const res = await api.put<ApiResult<{ enabled: boolean }>>(
    `/api/user/${userId}/strict_billing`,
    { enabled },
    { skipErrorHandler: true, skipBusinessError: true }
  )
  requireServerSuccess(res.data)
}
