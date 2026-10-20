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
import { useMutation, useQuery } from '@tanstack/react-query'
import { RefreshCw } from 'lucide-react'
import { useState } from 'react'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'

import {
  DataTablePagination,
  StaticDataTable,
  type StaticDataTableColumn,
  useDataTable,
} from '@/components/data-table'
import { Alert, AlertTitle } from '@/components/ui/alert'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { Switch } from '@/components/ui/switch'
import { useDebounce } from '@/hooks/use-debounce'
import { formatQuotaWithCurrency } from '@/lib/currency'
import { formatTimestamp } from '@/lib/format'
import { handleServerError } from '@/lib/handle-server-error'

import { getAbnormalChargeUsers, setUserStrictBilling } from './api'
import type { AbnormalChargeUser } from './types'

const FLAG_REASON_LABELS: Record<string, string> = {
  hourly_count: 'Many in one hour',
  ratio: 'High share',
  forgone_quota: 'Large amount',
}

type AbnormalUsersPanelProps = {
  days: number
  onShowRecords: (user: AbnormalChargeUser) => void
  onRecoverAll: (user: AbnormalChargeUser) => void
  onChanged: () => Promise<unknown>
}

export function AbnormalUsersPanel(props: AbnormalUsersPanelProps) {
  const { t } = useTranslation()
  const [keyword, setKeyword] = useState('')
  const debouncedKeyword = useDebounce(keyword.trim(), 400)
  const [pagination, setPagination] = useState({ pageIndex: 0, pageSize: 20 })

  const query = useQuery({
    queryKey: [
      'abnormal-charge-users',
      props.days,
      debouncedKeyword,
      pagination.pageIndex + 1,
      pagination.pageSize,
    ],
    queryFn: ({ signal }) =>
      getAbnormalChargeUsers(
        {
          days: props.days,
          keyword: debouncedKeyword,
          p: pagination.pageIndex + 1,
          page_size: pagination.pageSize,
        },
        signal
      ),
  })
  const users = query.data?.items ?? []
  const { table } = useDataTable({
    data: users,
    columns: [],
    totalCount: query.data?.total ?? 0,
    manualPagination: true,
    columnFilters: [],
    pagination,
    onPaginationChange: setPagination,
    columnVisibilityStorageKey: false,
    columnSizingStorageKey: false,
    ensurePageInRange: (pageCount) => {
      if (
        query.isSuccess &&
        !query.isFetching &&
        pagination.pageIndex >= Math.max(1, pageCount)
      ) {
        setPagination((previous) => ({
          ...previous,
          pageIndex: Math.max(0, pageCount - 1),
        }))
      }
    },
  })

  const strict = useMutation({
    mutationFn: (request: { userId: number; enabled: boolean }) =>
      setUserStrictBilling(request.userId, request.enabled),
    onSuccess: async (_result, request) => {
      toast.success(
        request.enabled
          ? t('Strict billing turned on')
          : t('Strict billing turned off')
      )
      await props.onChanged()
    },
    onError: (error) =>
      handleServerError(error, t('Failed to update strict billing')),
  })

  const columns: StaticDataTableColumn<AbnormalChargeUser>[] = [
    {
      id: 'user',
      header: t('User'),
      cell: (user) => (
        <div className='flex flex-col gap-1'>
          <span className='font-medium'>
            {user.username || '-'}{' '}
            <span className='text-muted-foreground text-xs'>
              #{user.user_id}
            </span>
          </span>
          <div className='flex flex-wrap gap-1'>
            {user.flagged ? (
              <Badge variant='destructive'>{t('Suspicious')}</Badge>
            ) : null}
            {(user.flag_reasons ?? []).map((reason) => (
              <Badge key={reason} variant='outline'>
                {t(FLAG_REASON_LABELS[reason] ?? reason)}
              </Badge>
            ))}
            {user.fixed_timing ? (
              <Badge variant='warning'>{t('Fixed timing')}</Badge>
            ) : null}
            {user.upstream_billed_count > 0 ? (
              <Badge variant='outline'>
                {t('Upstream charged {{number}}', {
                  number: user.upstream_billed_count,
                })}
              </Badge>
            ) : null}
          </div>
        </div>
      ),
    },
    {
      id: 'count',
      header: t('Abnormal requests'),
      cell: (user) => (
        <div className='flex flex-col gap-0.5 tabular-nums'>
          <span>{user.count}</span>
          <span className='text-muted-foreground text-xs'>
            {t('{{count}} received output', { count: user.received_count })}
          </span>
        </div>
      ),
    },
    {
      id: 'peak',
      header: t('Busiest hour'),
      cell: (user) => (
        <div className='flex flex-col gap-0.5 tabular-nums'>
          <span>{user.peak_hour_count}</span>
          <span className='text-muted-foreground text-xs'>
            {user.peak_hour_ratio_known
              ? t('{{percent}}% of {{requests}} requests', {
                  percent: user.peak_hour_ratio_percent.toFixed(0),
                  requests: user.peak_hour_requests,
                })
              : t('Share unknown')}
          </span>
        </div>
      ),
    },
    {
      id: 'elapsed',
      header: t('Disconnect time'),
      cell: (user) =>
        t('{{seconds}}s on average', {
          seconds: (user.avg_elapsed_ms / 1000).toFixed(1),
        }),
    },
    {
      id: 'amount',
      header: t('Uncollected'),
      cell: (user) => (
        <div className='flex flex-col gap-0.5 tabular-nums'>
          <span>{formatQuotaWithCurrency(user.pending_quota)}</span>
          <span className='text-muted-foreground text-xs'>
            {t('{{amount}} in total', {
              amount: formatQuotaWithCurrency(user.forgone_quota),
            })}
          </span>
        </div>
      ),
    },
    {
      id: 'last',
      header: t('Last seen'),
      cell: (user) => formatTimestamp(user.last_at),
    },
    {
      id: 'strict',
      header: t('Strict billing'),
      cell: (user) => (
        <Switch
          aria-label={t('Strict billing for {{username}}', {
            username: user.username || user.user_id,
          })}
          checked={user.strict_billing}
          disabled={strict.isPending}
          onCheckedChange={(enabled) =>
            strict.mutate({ userId: user.user_id, enabled })
          }
        />
      ),
    },
    {
      id: 'actions',
      header: <span className='sr-only'>{t('Actions')}</span>,
      className: 'text-right',
      cellClassName: 'text-right',
      cell: (user) => (
        <div className='flex justify-end gap-2'>
          <Button
            variant='outline'
            size='sm'
            onClick={() => props.onShowRecords(user)}
          >
            {t('Records')}
          </Button>
          <Button
            variant='destructive'
            size='sm'
            disabled={user.pending_count === 0}
            onClick={() => props.onRecoverAll(user)}
          >
            {t('Recover')}
          </Button>
        </div>
      ),
    },
  ]

  return (
    <div className='flex min-w-0 flex-col gap-3'>
      <div className='flex flex-wrap items-center gap-2'>
        <Input
          aria-label={t('Search by username or user ID')}
          placeholder={t('Search by username or user ID')}
          value={keyword}
          onChange={(event) => {
            setKeyword(event.target.value)
            setPagination((previous) => ({ ...previous, pageIndex: 0 }))
          }}
          className='w-64'
        />
        <Button
          variant='ghost'
          size='icon-sm'
          aria-label={t('Refresh')}
          disabled={query.isFetching}
          onClick={() => void query.refetch()}
        >
          <RefreshCw />
        </Button>
      </div>

      {query.isError ? (
        <Alert variant='destructive'>
          <AlertTitle>{t('Failed to load abnormal requests')}</AlertTitle>
        </Alert>
      ) : (
        <StaticDataTable
          columns={columns}
          data={users}
          getRowKey={(user) => user.user_id}
          emptyContent={
            query.isLoading
              ? t('Loading...')
              : t('No user left a request uncharged in this period')
          }
        />
      )}

      <DataTablePagination table={table} />
    </div>
  )
}
