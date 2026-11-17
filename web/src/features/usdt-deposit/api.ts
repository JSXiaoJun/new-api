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
  ApiEnvelope,
  PageResult,
  UsdtDeposit,
  UsdtInfo,
  UsdtOutcome,
  UsdtSettings,
  UsdtSettingsPatch,
} from './types'

async function unwrap<T>(request: Promise<{ data: ApiEnvelope<T> }>) {
  const res = await request
  return requireServerSuccess(res.data).data as T
}

// ---------------------------------------------------------------------------
// User
// ---------------------------------------------------------------------------

export function getUsdtInfo(): Promise<UsdtInfo> {
  return unwrap(api.get('/api/user/usdt/info'))
}

export function createUsdtAddress(
  chain: string
): Promise<{ chain: string; address: string }> {
  return unwrap(api.post('/api/user/usdt/address', { chain }))
}

export function getUsdtDeposits(
  page: number,
  pageSize: number
): Promise<PageResult<UsdtDeposit>> {
  return unwrap(
    api.get('/api/user/usdt/deposits', {
      params: { p: page, page_size: pageSize },
    })
  )
}

export function claimUsdtDeposit(
  chain: string,
  txHash: string
): Promise<UsdtOutcome[]> {
  return unwrap(api.post('/api/user/usdt/claim', { chain, tx_hash: txHash }))
}

// ---------------------------------------------------------------------------
// Administrator (root)
// ---------------------------------------------------------------------------

export function getUsdtSettings(): Promise<UsdtSettings> {
  return unwrap(api.get('/api/option/usdt'))
}

export function updateUsdtSettings(
  patch: UsdtSettingsPatch
): Promise<UsdtSettings> {
  return unwrap(api.put('/api/option/usdt', patch))
}

export function verifyUsdtFactory(
  chain: string
): Promise<{ implementation: string; destination: string }> {
  return unwrap(api.post('/api/option/usdt/verify', { chain }))
}

export function regenerateUsdtScannerSecret(): Promise<{ secret: string }> {
  return unwrap(api.post('/api/option/usdt/scanner-secret', {}))
}

export interface UsdtDepositFilters {
  user_id?: string
  chain?: string
  status?: string
  tx_hash?: string
}

export function listUsdtDeposits(
  page: number,
  pageSize: number,
  filters: UsdtDepositFilters
): Promise<PageResult<UsdtDeposit>> {
  return unwrap(
    api.get('/api/option/usdt/deposits', {
      params: { p: page, page_size: pageSize, ...filters },
    })
  )
}

export function recheckUsdtTransaction(
  chain: string,
  txHash: string
): Promise<UsdtOutcome[]> {
  return unwrap(
    api.post('/api/option/usdt/recheck', { chain, tx_hash: txHash })
  )
}

export function creditUsdtDeposit(id: number): Promise<UsdtOutcome[]> {
  return unwrap(api.post(`/api/option/usdt/deposits/${id}/credit`, {}))
}
