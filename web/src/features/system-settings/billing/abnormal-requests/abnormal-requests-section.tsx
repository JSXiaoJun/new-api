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
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { ChevronLeft, ChevronRight, RefreshCw } from 'lucide-react'
import { useState } from 'react'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'

import { ConfirmDialog } from '@/components/confirm-dialog'
import {
  StaticDataTable,
  type StaticDataTableColumn,
} from '@/components/data-table'
import { Alert, AlertTitle } from '@/components/ui/alert'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { NativeSelect, NativeSelectOption } from '@/components/ui/native-select'
import { Switch } from '@/components/ui/switch'
import { useDebounce } from '@/hooks/use-debounce'
import { formatQuotaWithCurrency } from '@/lib/currency'
import { formatTimestamp } from '@/lib/format'
import { handleServerError } from '@/lib/handle-server-error'

import { SettingsSection } from '../../components/settings-section'
import { AbnormalRecordsDialog } from './abnormal-records-dialog'
import {
  AbnormalRulesForm,
  type AbnormalRulesValues,
} from './abnormal-rules-form'
import {
  getAbnormalChargeUsers,
  recoverUserAbnormalCharges,
  setUserStrictBilling,
} from './api'
import type { AbnormalChargeRecord, AbnormalChargeUser } from './types'

const PERIODS = [1, 7, 30]
const PAGE_SIZE = 20

const FLAG_REASON_LABELS: Record<string, string> = {
  hourly_count: 'Many in one hour',
  ratio: 'High share',
  forgone_quota: 'Large amount',
}

type PendingRecovery = {
  user: AbnormalChargeUser
  records?: AbnormalChargeRecord[]
  count: number
  quota: number
}

export function AbnormalRequestsSection(props: {
  defaultValues: AbnormalRulesValues
}) {
  const { t } = useTranslation()
  const queryClient = useQueryClient()
  const [days, setDays] = useState(1)
  const [keyword, setKeyword] = useState('')
  const debouncedKeyword = useDebounce(keyword.trim(), 400)
  const [page, setPage] = useState(1)
  const [viewing, setViewing] = useState<AbnormalChargeUser | null>(null)
  const [pendingRecovery, setPendingRecovery] =
    useState<PendingRecovery | null>(null)

  const query = useQuery({
    queryKey: ['abnormal-charge-users', days, debouncedKeyword, page],
    queryFn: ({ signal }) =>
      getAbnormalChargeUsers(
        { days, keyword: debouncedKeyword, p: page, page_size: PAGE_SIZE },
        signal
      ),
  })
  const users = query.data?.items ?? []
  const totalPages = Math.max(
    1,
    Math.ceil((query.data?.total ?? 0) / PAGE_SIZE)
  )

  const refresh = () =>
    Promise.all([
      queryClient.invalidateQueries({ queryKey: ['abnormal-charge-users'] }),
      queryClient.invalidateQueries({ queryKey: ['abnormal-charge-records'] }),
    ])

  const recover = useMutation({
    mutationFn: (request: PendingRecovery) =>
      recoverUserAbnormalCharges(request.user.user_id, {
        ids: request.records?.map((record) => record.id),
        days,
        // Records that arrive after the admin reviewed the totals are not
        // charged in the same recovery.
        max_id: request.records ? undefined : request.user.max_pending_id,
      }),
    onSuccess: async (result) => {
      toast.success(
        t('Recovered {{amount}} from {{count}} records', {
          amount: formatQuotaWithCurrency(result.quota),
          count: result.recovered_records,
        })
      )
      setPendingRecovery(null)
      setViewing(null)
      await refresh()
    },
    onError: (error) =>
      handleServerError(error, t('Failed to recover charges')),
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
      await refresh()
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
          <Button variant='outline' size='sm' onClick={() => setViewing(user)}>
            {t('Records')}
          </Button>
          <Button
            variant='destructive'
            size='sm'
            disabled={user.pending_count === 0}
            onClick={() =>
              setPendingRecovery({
                user,
                count: user.pending_count,
                quota: user.pending_quota,
              })
            }
          >
            {t('Recover')}
          </Button>
        </div>
      ),
    },
  ]

  return (
    <SettingsSection title={t('Abnormal Requests')}>
      <AbnormalRulesForm defaultValues={props.defaultValues} />

      <div className='flex flex-wrap items-center gap-2'>
        <NativeSelect
          aria-label={t('Review period')}
          value={days}
          onChange={(event) => {
            setDays(Number(event.target.value))
            setPage(1)
          }}
        >
          {PERIODS.map((period) => (
            <NativeSelectOption key={period} value={period}>
              {t('Last {{days}} days', { days: period })}
            </NativeSelectOption>
          ))}
        </NativeSelect>
        <Input
          aria-label={t('Search by username or user ID')}
          placeholder={t('Search by username or user ID')}
          value={keyword}
          onChange={(event) => {
            setKeyword(event.target.value)
            setPage(1)
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

      <div className='flex items-center justify-end gap-2'>
        <Button
          variant='outline'
          size='icon-sm'
          aria-label={t('Go to previous page')}
          disabled={page <= 1 || query.isFetching}
          onClick={() => setPage((current) => current - 1)}
        >
          <ChevronLeft />
        </Button>
        <span
          className='text-muted-foreground text-sm tabular-nums'
          aria-live='polite'
        >
          {t('Page {{current}} of {{total}}', {
            current: page,
            total: totalPages,
          })}
        </span>
        <Button
          variant='outline'
          size='icon-sm'
          aria-label={t('Go to next page')}
          disabled={page >= totalPages || query.isFetching || query.isError}
          onClick={() => setPage((current) => current + 1)}
        >
          <ChevronRight />
        </Button>
      </div>

      {viewing ? (
        <AbnormalRecordsDialog
          open
          onOpenChange={(open) => !open && setViewing(null)}
          user={{ id: viewing.user_id, username: viewing.username }}
          days={days}
          onRecover={(records) =>
            setPendingRecovery({
              user: viewing,
              records,
              count: records.length,
              quota: records.reduce(
                (sum, record) => sum + record.forgone_quota,
                0
              ),
            })
          }
        />
      ) : null}

      <ConfirmDialog
        open={pendingRecovery !== null}
        onOpenChange={(open) =>
          !open && !recover.isPending && setPendingRecovery(null)
        }
        title={t('Recover uncollected charges')}
        desc={
          pendingRecovery
            ? t(
                'Charge {{username}} {{amount}} for {{count}} abnormal requests. The balance may become negative. Each record can be recovered only once.',
                {
                  username:
                    pendingRecovery.user.username ||
                    pendingRecovery.user.user_id,
                  amount: formatQuotaWithCurrency(pendingRecovery.quota),
                  count: pendingRecovery.count,
                }
              )
            : ''
        }
        confirmText={t('Recover')}
        destructive
        isLoading={recover.isPending}
        handleConfirm={() => pendingRecovery && recover.mutate(pendingRecovery)}
      />
    </SettingsSection>
  )
}
