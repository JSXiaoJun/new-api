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
import type { ColumnDef } from '@tanstack/react-table'
import { useMemo } from 'react'
import { useTranslation } from 'react-i18next'

import { LongText } from '@/components/long-text'
import { StatusBadge, type StatusVariant } from '@/components/status-badge'
import { formatNumber, formatTimestampToDate } from '@/lib/format'

import type { TopUpOrder, TopUpOrderStatus } from '../types'

const STATUS_VARIANTS: Record<TopUpOrderStatus, StatusVariant> = {
  success: 'success',
  pending: 'warning',
  failed: 'danger',
  expired: 'neutral',
}

const STATUS_LABELS: Record<TopUpOrderStatus, string> = {
  success: 'Success',
  pending: 'Pending',
  failed: 'Failed',
  expired: 'Expired',
}

function formatPaymentChannel(order: TopUpOrder): string {
  const provider = order.payment_provider?.trim()
  const method = order.payment_method?.trim()
  if (provider && method && provider !== method) {
    return `${provider} / ${method}`
  }
  return provider || method || '-'
}

export function useTopUpOrdersColumns(): ColumnDef<TopUpOrder>[] {
  const { t } = useTranslation()

  return useMemo(
    () => [
      {
        accessorKey: 'trade_no',
        header: t('Order Number'),
        cell: ({ row }) => (
          <StatusBadge
            label={row.original.trade_no}
            copyText={row.original.trade_no}
            variant='neutral'
            className='max-w-[260px] font-mono'
          />
        ),
        size: 280,
        meta: { mobileTitle: true },
      },
      {
        accessorKey: 'username',
        header: t('User'),
        cell: ({ row }) => {
          const order = row.original
          return (
            <div className='flex min-w-[180px] flex-col gap-0.5'>
              <div className='flex items-center gap-2'>
                <LongText className='max-w-[140px] font-medium'>
                  {order.username || t('Deleted User')}
                </LongText>
                <StatusBadge
                  label={`ID: ${order.user_id}`}
                  copyText={String(order.user_id)}
                  variant='neutral'
                />
              </div>
              {order.display_name && order.display_name !== order.username ? (
                <LongText className='text-muted-foreground max-w-[180px] text-xs'>
                  {order.display_name}
                </LongText>
              ) : null}
              {order.email ? (
                <LongText className='text-muted-foreground max-w-[180px] text-xs'>
                  {order.email}
                </LongText>
              ) : null}
            </div>
          )
        },
        size: 220,
        meta: { mobileOrder: 10 },
      },
      {
        accessorKey: 'status',
        header: t('Status'),
        cell: ({ row }) => (
          <StatusBadge
            label={t(STATUS_LABELS[row.original.status])}
            variant={STATUS_VARIANTS[row.original.status]}
            copyable={false}
          />
        ),
        size: 100,
        meta: { mobileBadge: true },
      },
      {
        id: 'payment_channel',
        header: t('Payment Channel'),
        cell: ({ row }) => (
          <span className='text-sm'>{formatPaymentChannel(row.original)}</span>
        ),
        size: 150,
        meta: { mobileOrder: 20 },
      },
      {
        accessorKey: 'amount',
        header: t('Top-up Amount'),
        cell: ({ row }) => (
          <span className='font-medium'>
            {formatNumber(row.original.amount)}
          </span>
        ),
        size: 120,
        meta: { mobileOrder: 30 },
      },
      {
        accessorKey: 'money',
        header: t('Paid Amount'),
        cell: ({ row }) => (
          <span className='font-medium'>
            {formatNumber(row.original.money)}
          </span>
        ),
        size: 120,
        meta: { mobileOrder: 40 },
      },
      {
        accessorKey: 'create_time',
        header: t('Created At'),
        cell: ({ row }) => (
          <span className='text-muted-foreground text-sm'>
            {formatTimestampToDate(row.original.create_time)}
          </span>
        ),
        size: 170,
        meta: { mobileHidden: true },
      },
      {
        accessorKey: 'complete_time',
        header: t('Completed At'),
        cell: ({ row }) => (
          <span className='text-muted-foreground text-sm'>
            {row.original.complete_time
              ? formatTimestampToDate(row.original.complete_time)
              : '-'}
          </span>
        ),
        size: 170,
        meta: { mobileHidden: true },
      },
    ],
    [t]
  )
}
