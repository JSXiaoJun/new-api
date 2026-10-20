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
export type ApiResult<T> = { success: boolean; message?: string; data: T }

export type ApiPage<T> = {
  page: number
  page_size: number
  total: number
  items: T[]
}

export type AbnormalFlagReason = 'hourly_count' | 'ratio' | 'forgone_quota'

export type AbnormalChargeUser = {
  user_id: number
  username: string
  count: number
  received_count: number
  forgone_quota: number
  pending_quota: number
  pending_count: number
  max_pending_id: number
  upstream_billed_count: number
  last_at: number
  avg_elapsed_ms: number
  elapsed_stddev_ms: number
  peak_hour_count: number
  peak_hour_weighted: number
  peak_hour_requests: number
  peak_hour_ratio_known: boolean
  peak_hour_ratio_percent: number
  fixed_timing: boolean
  flagged: boolean
  flag_reasons: AbnormalFlagReason[] | null
  strict_billing: boolean
}

export type AbnormalChargeRecord = {
  id: number
  user_id: number
  created_at: number
  request_id: string
  token_name: string
  model_name: string
  channel_id: number
  ip: string
  is_stream: boolean
  is_task: boolean
  case: string
  user_caused: boolean
  ended_by: string
  phase: string
  reason: string
  elapsed_ms: number
  client_received_data: boolean
  outcome: 'waived' | 'refunded' | 'partial'
  forgone_quota: number
  upstream_billing: '' | 'billed' | 'unknown'
  upstream_quota: number
  upstream_prompt_tokens: number
  upstream_completion_tokens: number
  charged_quota: number
  recovery_id: string
  recovered_at: number
}

export type AbnormalChargeRecordStatus = '' | 'pending' | 'recovered'

export type AbnormalRecovery = {
  recovered_records: number
  quota: number
  from: number
  to: number
}
