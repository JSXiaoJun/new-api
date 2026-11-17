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
import { useQuery } from '@tanstack/react-query'
import { type ReactNode, useState } from 'react'
import { useTranslation } from 'react-i18next'

import { StatusBadge } from '@/components/status-badge'
import { Button } from '@/components/ui/button'
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from '@/components/ui/table'
import { formatQuota, formatTimestamp } from '@/lib/format'

import { getUsdtDeposits } from '../api'
import { DEPOSIT_STATUS, USDT_QUERY_KEYS } from '../lib'
import type { UsdtDeposit } from '../types'

const PAGE_SIZE = 10

export function UsdtDepositHistory() {
  const { t } = useTranslation()
  const [page, setPage] = useState(1)
  const query = useQuery({
    queryKey: USDT_QUERY_KEYS.deposits(page),
    queryFn: () => getUsdtDeposits(page, PAGE_SIZE),
    refetchInterval: 15_000,
  })
  const items = query.data?.items ?? []
  const total = query.data?.total ?? 0
  const lastPage = Math.max(1, Math.ceil(total / PAGE_SIZE))

  return (
    <section className='space-y-2' aria-label={t('USDT deposit history')}>
      <div className='flex items-center justify-between'>
        <h3 className='text-sm font-medium'>{t('USDT deposit history')}</h3>
        <span className='text-muted-foreground text-xs'>
          {t('Refreshes every 15 seconds')}
        </span>
      </div>
      {items.length === 0 ? (
        <p className='text-muted-foreground rounded-lg border border-dashed p-4 text-center text-sm'>
          {t('No USDT deposits yet')}
        </p>
      ) : (
        <Table>
          <TableHeader>
            <TableRow>
              <TableHead>{t('Network')}</TableHead>
              <TableHead>{t('Amount')}</TableHead>
              <TableHead>{t('Credited')}</TableHead>
              <TableHead>{t('Status')}</TableHead>
              <TableHead>{t('Transaction')}</TableHead>
              <TableHead>{t('Time')}</TableHead>
            </TableRow>
          </TableHeader>
          <TableBody>
            {items.map((item) => (
              <DepositRow key={item.id} item={item} />
            ))}
          </TableBody>
        </Table>
      )}
      {lastPage > 1 && (
        <div className='flex justify-end gap-2'>
          <Button
            size='sm'
            variant='outline'
            disabled={page <= 1}
            onClick={() => setPage((p) => p - 1)}
          >
            {t('Previous')}
          </Button>
          <Button
            size='sm'
            variant='outline'
            disabled={page >= lastPage}
            onClick={() => setPage((p) => p + 1)}
          >
            {t('Next')}
          </Button>
        </div>
      )}
    </section>
  )
}

export function DepositRow(props: {
  item: UsdtDeposit
  showUser?: boolean
  action?: ReactNode
}) {
  const { t } = useTranslation()
  const status = DEPOSIT_STATUS[props.item.status]
  return (
    <TableRow>
      {props.showUser && <TableCell>{props.item.user_id}</TableCell>}
      <TableCell className='whitespace-nowrap'>
        {props.item.chain.toUpperCase()}
      </TableCell>
      <TableCell className='whitespace-nowrap'>
        {props.item.amount} USDT
      </TableCell>
      <TableCell className='whitespace-nowrap'>
        {props.item.status === 'credited' ? formatQuota(props.item.quota) : '-'}
      </TableCell>
      <TableCell>
        <StatusBadge
          label={status ? t(status.labelKey) : props.item.status}
          variant={status?.variant ?? 'neutral'}
        />
      </TableCell>
      <TableCell className='max-w-48'>
        <a
          href={props.item.tx_url}
          target='_blank'
          rel='noopener noreferrer'
          className='text-primary block truncate font-mono text-xs hover:underline'
          title={props.item.tx_hash}
        >
          {props.item.tx_hash}
        </a>
      </TableCell>
      <TableCell className='whitespace-nowrap'>
        {formatTimestamp(props.item.created_at)}
      </TableCell>
      {props.action}
    </TableRow>
  )
}
