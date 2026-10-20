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
import { beforeEach, expect, test } from 'vitest'

import {
  DEFAULT_CURRENCY_CONFIG,
  useSystemConfigStore,
} from '@/stores/system-config-store'

import type { UserQuotaLog } from '../../types'
import { getHistoricalWalletChange } from '../wallet-change'

const topUpKey =
  'Online top-up succeeded, amount added: {{quota}}, payment amount: {{amount}}'

function log(
  fields: Partial<Omit<UserQuotaLog, 'other'>> & { other?: unknown }
): UserQuotaLog {
  return {
    id: 1,
    user_id: 8927,
    username: 'wallet-owner',
    created_at: 1787124215,
    type: 1,
    quota: 0,
    content: '',
    ...fields,
    other: fields.other === undefined ? '' : JSON.stringify(fields.other),
  }
}

beforeEach(() => {
  // 500000 quota units per USD.
  useSystemConfigStore
    .getState()
    .setConfig({ currency: { ...DEFAULT_CURRENCY_CONFIG } })
})

test.each([
  [
    'an epay top-up shows the credited quota and the payment',
    log({
      other: {
        content_parts: [
          {
            key: topUpKey,
            params: { quota: '¥70.000000', amount: '10.000000' },
          },
        ],
        admin_info: { callback_payment_method: 'epay' },
      },
    }),
    { quota: '+¥70', paid: { amount: 10 } },
  ],
  [
    'a Creem top-up with a raw quota shows it in the display currency',
    log({
      other: {
        content_parts: [
          {
            key: '{{provider}} top-up succeeded, amount added: {{quota}}, payment amount: {{amount}}',
            params: { provider: 'Creem', quota: 2500000, amount: '4.99' },
          },
        ],
      },
    }),
    { quota: '+$5', paid: { amount: 4.99 } },
  ],
  [
    'a Stripe top-up that logged the purchased quantity has no trusted payment',
    log({
      other: {
        content_parts: [
          { key: topUpKey, params: { quota: '＄20.000000', amount: 20 } },
        ],
        admin_info: { callback_payment_method: 'stripe' },
      },
    }),
    { quota: '+＄20', paid: 'unrecorded' },
  ],
  [
    'a Stripe top-up shows the charged total in its currency',
    log({
      other: {
        content_parts: [
          {
            key: topUpKey,
            params: { quota: '＄20.000000', amount: '146.00', currency: 'CNY' },
          },
        ],
        admin_info: { callback_payment_method: 'stripe' },
      },
    }),
    { quota: '+＄20', paid: { amount: 146, currency: 'CNY' } },
  ],
  [
    'a Stripe top-up in a zero-decimal currency shows the whole amount',
    log({
      other: {
        content_parts: [
          {
            key: topUpKey,
            params: { quota: '＄20.000000', amount: '3000', currency: 'JPY' },
          },
        ],
      },
    }),
    { quota: '+＄20', paid: { amount: 3000, currency: 'JPY' } },
  ],
  [
    'a Stripe top-up whose total was not reported has no payment',
    log({
      other: {
        content_parts: [
          { key: topUpKey, params: { quota: '＄20.000000', amount: '-' } },
        ],
      },
    }),
    { quota: '+＄20', paid: 'unrecorded' },
  ],
  [
    'an order an administrator completed shows the payment',
    log({
      other: {
        content_parts: [
          {
            key: 'Administrator completed the order, amount added: {{quota}}, payment amount: {{amount}}',
            params: { quota: '¥35.000000', amount: '5.000000' },
          },
        ],
      },
    }),
    { quota: '+¥35', paid: { amount: 5 } },
  ],
  [
    'a redemption code credits quota without a payment',
    log({
      other: {
        content_parts: [
          {
            key: 'Topped up {{quota}} with a redemption code, code ID {{id}}',
            params: { quota: '¥7.500000', id: 3 },
          },
        ],
      },
    }),
    { quota: '+¥7.5', paid: null },
  ],
  [
    'an administrator credit is the balance difference',
    log({
      other: {
        op: {
          action: 'user.quota_add',
          params: { mode: 'add', quota: 5000000, from: 1000000, to: 6000000 },
        },
      },
    }),
    { quota: '+$10', paid: null },
  ],
  [
    'an administrator override that lowered the balance is negative',
    log({
      other: {
        op: {
          action: 'user.quota_override',
          params: { mode: 'override', from: 6000000, to: 1000000 },
        },
      },
    }),
    { quota: '-$10', paid: null },
  ],
  [
    'a failed administrator credit changed nothing',
    log({
      type: 3,
      other: {
        op: { action: 'user.quota_add', params: { quota: 5000000 } },
        audit_info: { success: false },
      },
    }),
    { quota: null, paid: null },
  ],
  [
    'a subscription paid from the wallet is not a credit',
    log({
      other: {
        content_parts: [
          {
            key: 'Subscription purchased with balance, plan: {{plan}}, payment amount: {{amount}}, quota deducted: {{quota}}',
            params: { plan: 'Pro', amount: '5.00', quota: '$5.000000' },
          },
        ],
      },
    }),
    { quota: null, paid: null },
  ],
])('%s', (_name, record, expected) => {
  expect(getHistoricalWalletChange(record)).toEqual(expected)
})

test.each([
  [
    '使用在线充值成功，充值金额: ¥70.000000 额度，支付金额：10.000000',
    { quota: '+¥70', paid: { amount: 10 } },
  ],
  [
    'Waffo充值成功，充值额度: ¥14.000000，支付金额: 2.00',
    { quota: '+¥14', paid: { amount: 2 } },
  ],
  [
    '使用在线充值成功，充值金额: ＄20.000000 额度，支付金额：20',
    { quota: '+＄20', paid: 'unrecorded' },
  ],
  [
    '使用在线充值成功，充值金额: 2500000 点额度',
    { quota: '+$5', paid: 'unrecorded' },
  ],
  ['通过兑换码充值 ¥7.000000 额度，兑换码ID 12', { quota: '+¥7', paid: null }],
])(
  'a legacy top-up recorded as "%s" keeps its amounts',
  (content, expected) => {
    expect(getHistoricalWalletChange(log({ content }))).toEqual(expected)
  }
)

test.each([
  ['管理员增加用户额度 ¥10.000000 额度', '+¥10'],
  ['管理员减少用户额度 500000 点额度', '-$1'],
  ['管理员覆盖用户额度从 ¥4.000000 额度 为 ¥12.000000 额度', '¥4 → ¥12'],
])(
  'a legacy administrator adjustment recorded as "%s" shows %s',
  (content, quota) => {
    expect(getHistoricalWalletChange(log({ type: 3, content }))).toEqual({
      quota,
      paid: null,
    })
  }
)

test('a legacy subscription purchase is not read as a top-up', () => {
  expect(
    getHistoricalWalletChange(
      log({
        content: '订阅购买成功，套餐: Pro，支付金额: 9.90，支付方式: alipay',
      })
    )
  ).toEqual({ quota: null, paid: null })
})

test('consumption keeps its signed raw quota and no payment', () => {
  expect(getHistoricalWalletChange(log({ type: 2, quota: -750000 }))).toEqual({
    quota: '-750000',
    paid: null,
  })
})
