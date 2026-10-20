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
import { parseLogOther } from '@/features/usage-logs/lib/format'
import { formatQuotaWithCurrency } from '@/lib/currency'

import type { UserQuotaLog } from '../types'

/**
 * What a historical log says about the wallet. `quota` is the signed change
 * shown to the administrator, or null when the log does not identify it.
 * `paid` is what the user paid the gateway for a top-up, in the currency the
 * log names or else the gateway's own; 'unrecorded' when the top-up log holds
 * no trustworthy payment amount, or null when no payment was involved.
 */
export type PaidAmount = { amount: number; currency?: string } | 'unrecorded'

export interface HistoricalWalletChange {
  quota: string | null
  paid: PaidAmount | null
}

// Gateway messages whose `quota` param is the amount credited to the wallet.
// Those with an `amount` param also record the payment.
const WALLET_CREDIT_MESSAGE_KEYS = new Set([
  'Online top-up succeeded, amount added: {{quota}}, payment amount: {{amount}}',
  '{{provider}} top-up succeeded, amount added: {{quota}}, payment amount: {{amount}}',
  'Administrator completed the order, amount added: {{quota}}, payment amount: {{amount}}',
  'Topped up {{quota}} with a redemption code, code ID {{id}}',
])

// Logs written before structured content keep only this Chinese text.
const LEGACY_TOP_UP =
  /充值成功，充值(?:额度|金额)[:：]\s*(.+?)(?:，支付金额[:：]\s*([\d.]+))?$/
const LEGACY_REDEMPTION = /^通过兑换码充值\s*(.+?)(?:，兑换码ID\s*\d+)?$/
const LEGACY_ADMIN_ADD = /^管理员增加用户额度\s*(.+)$/
const LEGACY_ADMIN_SUBTRACT = /^管理员减少用户额度\s*(.+)$/
const LEGACY_ADMIN_OVERRIDE = /^管理员覆盖用户额度从\s*(.+?)\s*为\s*(.+)$/

/**
 * Normalize an amount the server already formatted. Old logs append a unit
 * ("¥7.000000 额度", "500000 点额度"); a bare number is raw quota.
 */
function recordedQuota(value: string): string {
  const amount = value
    .trim()
    .replace(/\s*点?额度$/, '')
    .replace(/(\.\d*?)0+$/, '$1')
    .replace(/\.$/, '')
  if (/^\d+$/.test(amount)) return formatQuotaWithCurrency(Number(amount))
  return amount
}

function signedQuota(value: unknown, sign: '+' | '-'): string | null {
  if (typeof value === 'number' && Number.isFinite(value)) {
    return `${sign}${formatQuotaWithCurrency(Math.abs(value))}`
  }
  if (typeof value === 'string' && value.trim() !== '') {
    return `${sign}${recordedQuota(value)}`
  }
  return null
}

/**
 * Payment amounts are written with decimals, or with the currency Stripe
 * charged in (zero-decimal currencies such as JPY have none). An integer
 * without a currency is the purchased quantity Stripe top-ups logged before
 * the charge was recorded, so it is not trusted as a payment.
 */
function paidAmount(value: unknown, currency?: unknown): PaidAmount {
  const amount = typeof value === 'string' ? value.trim() : ''
  if (typeof currency === 'string' && /^[A-Z]{3}$/.test(currency)) {
    if (!/^\d+(\.\d+)?$/.test(amount)) return 'unrecorded'
    return { amount: Number(amount), currency }
  }
  if (!/^\d+\.\d+$/.test(amount)) return 'unrecorded'
  return { amount: Number(amount) }
}

export function getHistoricalWalletChange(
  record: UserQuotaLog
): HistoricalWalletChange {
  if (record.quota !== 0 && (record.type === 2 || record.type === 6)) {
    return { quota: String(record.quota), paid: null }
  }
  const other = parseLogOther(record.other)
  const op = other?.op
  if (op?.action?.startsWith('user.quota_')) {
    if (other?.audit_info?.success === false) return { quota: null, paid: null }
    const params = op.params ?? {}
    if (typeof params.from === 'number' && typeof params.to === 'number') {
      const delta = params.to - params.from
      return { quota: signedQuota(delta, delta < 0 ? '-' : '+'), paid: null }
    }
    if (op.action === 'user.quota_add') {
      return { quota: signedQuota(params.quota, '+'), paid: null }
    }
    if (op.action === 'user.quota_subtract') {
      return { quota: signedQuota(params.quota, '-'), paid: null }
    }
    if (
      op.action === 'user.quota_override' &&
      typeof params.from === 'string' &&
      typeof params.to === 'string'
    ) {
      return { quota: `${params.from} → ${params.to}`, paid: null }
    }
    return { quota: null, paid: null }
  }

  const part = other?.content_parts?.[0]
  if (part) {
    if (!WALLET_CREDIT_MESSAGE_KEYS.has(part.key)) {
      return { quota: null, paid: null }
    }
    return {
      quota: signedQuota(part.params?.quota, '+'),
      paid: part.key.includes('{{amount}}')
        ? paidAmount(part.params?.amount, part.params?.currency)
        : null,
    }
  }

  const content = record.content.trim()
  const redemption = LEGACY_REDEMPTION.exec(content)
  if (redemption) {
    return { quota: signedQuota(redemption[1], '+'), paid: null }
  }
  const topUp = record.type === 1 ? LEGACY_TOP_UP.exec(content) : null
  if (topUp) {
    return { quota: signedQuota(topUp[1], '+'), paid: paidAmount(topUp[2]) }
  }
  const added = LEGACY_ADMIN_ADD.exec(content)
  if (added) return { quota: signedQuota(added[1], '+'), paid: null }
  const subtracted = LEGACY_ADMIN_SUBTRACT.exec(content)
  if (subtracted) return { quota: signedQuota(subtracted[1], '-'), paid: null }
  const overridden = LEGACY_ADMIN_OVERRIDE.exec(content)
  if (overridden) {
    return {
      quota: `${recordedQuota(overridden[1])} → ${recordedQuota(overridden[2])}`,
      paid: null,
    }
  }
  return { quota: null, paid: null }
}
