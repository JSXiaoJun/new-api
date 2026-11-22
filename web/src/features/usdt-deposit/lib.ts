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
import type { StatusVariant } from '@/components/status-badge'

import type { UsdtDepositStatus, UsdtOutcome } from './types'

export const USDT_QUERY_KEYS = {
  info: ['usdt', 'info'] as const,
  deposits: ['usdt', 'deposits'] as const,
  settings: ['usdt', 'settings'] as const,
  adminDeposits: (page: number, filters: string) =>
    ['usdt', 'admin-deposits', page, filters] as const,
}

/** i18n keys and badge colors for a recorded deposit. */
export const DEPOSIT_STATUS: Record<
  UsdtDepositStatus,
  { labelKey: string; variant: StatusVariant }
> = {
  credited: { labelKey: 'Credited', variant: 'success' },
  below_min: { labelKey: 'Below minimum', variant: 'warning' },
}

/** A transaction hash is 64 hex characters, with or without 0x (TRON txids). */
export function isTxHash(value: string): boolean {
  return /^(0x)?[0-9a-fA-F]{64}$/.test(value.trim())
}

export interface ClaimSummary {
  creditedQuota: number
  credited: number
  already: number
  belowMin: number
}

/** Summarizes the outcomes of one checked transaction. */
export function summarizeOutcomes(outcomes: UsdtOutcome[]): ClaimSummary {
  const summary: ClaimSummary = {
    creditedQuota: 0,
    credited: 0,
    already: 0,
    belowMin: 0,
  }
  for (const outcome of outcomes) {
    if (outcome.status === 'credited') {
      summary.credited += 1
      summary.creditedQuota += outcome.quota ?? 0
    } else if (outcome.status === 'already') {
      summary.already += 1
    } else if (outcome.status === 'below_min') {
      summary.belowMin += 1
    }
  }
  return summary
}
