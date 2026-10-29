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
import { createInstance } from 'i18next'
import { describe, expect, test } from 'vitest'

import type { TopUpOrder } from '../../types'
import { getTopUpOrderCsvHeader, getTopUpOrderCsvRow } from '../export'

const order: TopUpOrder = {
  id: 1,
  user_id: 9200,
  username: '333',
  display_name: '',
  email: '',
  amount: 10,
  money: 9.5,
  trade_no: 'USR9200NODG63bM1793278008',
  payment_method: 'wxpay',
  payment_provider: 'epay',
  create_time: new Date(2026, 9, 29, 20, 46, 48).getTime() / 1000,
  complete_time: new Date(2026, 9, 29, 20, 47, 2).getTime() / 1000,
  status: 'success',
}

async function chineseTranslator() {
  const i18n = createInstance()
  await i18n.init({
    lng: 'zh',
    resources: {
      zh: {
        translation: {
          'Order Number': '订单号',
          User: '用户',
          'Payment Channel': '支付渠道',
          'Top-up Amount': '充值金额',
          'Paid Amount': '实付金额',
          'Created At': '创建时间',
          'Completed At': '完成时间',
          'Deleted User': '已删除用户',
        },
      },
    },
  })
  return i18n.t
}

describe('recharge order CSV export', () => {
  test('header lists the requested columns in order', async () => {
    const t = await chineseTranslator()

    expect(getTopUpOrderCsvHeader(t)).toEqual([
      '订单号',
      '用户',
      '支付渠道',
      '充值金额',
      '实付金额',
      '创建时间',
      '完成时间',
    ])
  })

  test('completed order row matches the order table values', async () => {
    const t = await chineseTranslator()

    expect(getTopUpOrderCsvRow(order, t)).toEqual([
      'USR9200NODG63bM1793278008',
      '333 (ID: 9200)',
      'epay / wxpay',
      '10',
      '9.5',
      '2026-10-29 20:46:48',
      '2026-10-29 20:47:02',
    ])
  })

  test('pending order of a deleted user has an empty completion time', async () => {
    const t = await chineseTranslator()

    const row = getTopUpOrderCsvRow(
      { ...order, username: '', complete_time: 0, status: 'pending' },
      t
    )

    expect(row[1]).toBe('已删除用户 (ID: 9200)')
    expect(row[6]).toBe('')
  })
})
