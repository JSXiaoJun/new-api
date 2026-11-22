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
export interface ApiEnvelope<T> {
  success: boolean
  message?: string
  data?: T
}

export interface PageResult<T> {
  page: number
  page_size: number
  total: number
  items: T[]
}

export interface UsdtNetwork {
  id: string
  name: string
  min_amount: string
  contract: string
  tx_url: string
}

export interface UsdtInfo {
  enabled: boolean
  networks: UsdtNetwork[] | null
  /** network id -> the user's permanent deposit address */
  addresses: Record<string, string>
  rate?: string
  rate_source?: string
  rate_mode: string
}

export type UsdtDepositStatus = 'credited' | 'below_min'

export interface UsdtDeposit {
  id: number
  chain: string
  tx_hash: string
  log_index: number
  user_id: number
  address: string
  address_display: string
  from_address: string
  amount: string
  rate: string
  quota: number
  status: UsdtDepositStatus
  source: string
  created_at: number
  credited_at: number
  tx_url: string
}

export type UsdtOutcomeStatus =
  | 'credited'
  | 'below_min'
  | 'already'
  | 'not_ours'

export interface UsdtOutcome {
  chain: string
  tx_hash: string
  log_index: number
  status: UsdtOutcomeStatus
  user_id?: number
  amount: string
  quota?: number
}

export type UsdtFinality = 'finalized' | 'safe' | 'confirmations'

/** market: USDT/USD; fixed: admin rate; c2c: OKX / Binance CNY C2C price */
export type UsdtRateMode = 'market' | 'fixed' | 'c2c'

export interface UsdtChainSetting {
  id: string
  name: string
  contract: string
  enabled: boolean
  factory_address: string
  factory_display: string
  implementation_address: string
  verified: boolean
  rpc_url: string
  rpc_url_secondary: string
  finality: UsdtFinality
  confirmations: number
  min_amount: number
  sweep_threshold: number
  address_count: number
  factory_locked: boolean
}

export interface UsdtChainHeartbeat {
  head: number
  cursor: number
  gas_balance: string
  pending_sweeps: number
  last_error: string
}

export interface UsdtVerificationAlert {
  at: number
  chain: string
  tx_hash: string
  detail: string
}

export interface UsdtSettings {
  enabled: boolean
  admin_only: boolean
  rate_mode: UsdtRateMode
  fixed_rate: number
  scanner_ips: string
  has_scanner_secret: boolean
  chains: UsdtChainSetting[]
  heartbeat: Record<string, UsdtChainHeartbeat> | null
  heartbeat_at: number
  scanner_version: string
  alerts: UsdtVerificationAlert[] | null
}

export interface UsdtChainPatch {
  enabled?: boolean
  factory_address?: string
  rpc_url?: string
  rpc_url_secondary?: string
  finality?: UsdtFinality
  confirmations?: number
  min_amount?: number
  sweep_threshold?: number
}

export interface UsdtSettingsPatch {
  enabled?: boolean
  admin_only?: boolean
  rate_mode?: UsdtRateMode
  fixed_rate?: number
  scanner_ips?: string
  chains?: Record<string, UsdtChainPatch>
}
