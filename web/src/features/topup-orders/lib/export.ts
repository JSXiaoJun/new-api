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
import { formatTimestampToDate } from '@/lib/format'

import type { TopUpOrder } from '../types'

type Translate = (key: string, options?: Record<string, unknown>) => string

/** The payment provider and method of an order, as the order table shows it. */
export function formatPaymentChannel(order: TopUpOrder): string {
  const provider = order.payment_provider?.trim()
  const method = order.payment_method?.trim()
  if (provider && method && provider !== method) {
    return `${provider} / ${method}`
  }
  return provider || method || '-'
}

/** Column titles of the exported recharge order file. */
export function getTopUpOrderCsvHeader(t: Translate): string[] {
  return [
    t('Order Number'),
    t('User'),
    t('Payment Channel'),
    t('Top-up Amount'),
    t('Paid Amount'),
    t('Created At'),
    t('Completed At'),
  ]
}

/**
 * One order as the cells under getTopUpOrderCsvHeader. Amounts stay plain
 * numbers so spreadsheets can sum them, and an unfinished order has an empty
 * completion time.
 */
export function getTopUpOrderCsvRow(order: TopUpOrder, t: Translate): string[] {
  const username = order.username || t('Deleted User')
  return [
    order.trade_no,
    `${username} (ID: ${order.user_id})`,
    formatPaymentChannel(order),
    String(order.amount),
    String(order.money),
    formatTimestampToDate(order.create_time),
    order.complete_time ? formatTimestampToDate(order.complete_time) : '',
  ]
}
