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
import { RefreshCw } from 'lucide-react'
import { type ReactNode, useEffect, useRef, useState } from 'react'
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
import { cn } from '@/lib/utils'

import { getUsdtDeposits } from '../api'
import { DEPOSIT_STATUS, USDT_QUERY_KEYS } from '../lib'
import type { UsdtDeposit } from '../types'

/** How long the refresh button stays disabled after a click. */
export const USDT_REFRESH_COOLDOWN_MS = 3_000
/** Shortest spin of the refresh icon, so a fast refresh is still visible. */
const MIN_SPIN_MS = 800

/**
 * The user's latest USDT deposits. The list refreshes itself every 15
 * seconds and on demand, and reports newly credited deposits so the wallet
 * can refresh the balance.
 */
export function UsdtDepositHistory(props: { onCredited: () => void }) {
  const { t } = useTranslation()
  const [coolingDown, setCoolingDown] = useState(false)
  const [spinning, setSpinning] = useState(false)
  const query = useQuery({
    queryKey: USDT_QUERY_KEYS.deposits,
    queryFn: getUsdtDeposits,
    refetchInterval: 15_000,
  })
  const items = query.data ?? []
  const creditedIdsRef = useRef<Set<number> | null>(null)
  const onCredited = props.onCredited

  useEffect(() => {
    if (!query.data) return
    const credited = new Set(
      query.data.filter((d) => d.status === 'credited').map((d) => d.id)
    )
    const previous = creditedIdsRef.current
    creditedIdsRef.current = credited
    if (previous && [...credited].some((id) => !previous.has(id))) {
      onCredited()
    }
  }, [query.data, onCredited])

  useEffect(() => {
    if (query.isFetching) {
      setSpinning(true)
      return
    }
    const timer = window.setTimeout(() => setSpinning(false), MIN_SPIN_MS)
    return () => window.clearTimeout(timer)
  }, [query.isFetching])

  useEffect(() => {
    if (!coolingDown) return
    const timer = window.setTimeout(
      () => setCoolingDown(false),
      USDT_REFRESH_COOLDOWN_MS
    )
    return () => window.clearTimeout(timer)
  }, [coolingDown])

  return (
    <section className='space-y-2' aria-label={t('USDT deposit history')}>
      <div className='flex items-center justify-between gap-2'>
        <h3 className='text-sm font-medium'>
          {t('USDT deposit history')}
          <span className='text-muted-foreground ms-2 text-xs font-normal'>
            {t('Latest 10 only')}
          </span>
        </h3>
        <div className='text-muted-foreground flex items-center gap-1 text-xs'>
          <Button
            type='button'
            variant='ghost'
            size='icon'
            className='text-muted-foreground hover:text-foreground size-7'
            aria-label={t('Refresh')}
            title={t('Refresh')}
            aria-busy={query.isFetching}
            disabled={spinning || coolingDown}
            onClick={() => {
              setCoolingDown(true)
              void query.refetch()
            }}
          >
            <RefreshCw
              aria-hidden='true'
              className={cn(
                'size-3.5',
                spinning && 'animate-spin motion-reduce:animate-none'
              )}
            />
          </Button>
          <span>{t('Refreshes every 15 seconds')}</span>
        </div>
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
