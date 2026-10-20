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
import { useState } from 'react'
import { useTranslation } from 'react-i18next'

import {
  DataTablePagination,
  StaticDataTable,
  type StaticDataTableColumn,
  useDataTable,
} from '@/components/data-table'
import { Alert, AlertTitle } from '@/components/ui/alert'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import { Checkbox } from '@/components/ui/checkbox'
import {
  ComboboxInput,
  type ComboboxInputOption,
} from '@/components/ui/combobox-input'
import { NativeSelect, NativeSelectOption } from '@/components/ui/native-select'
import { useDebounce } from '@/hooks/use-debounce'
import { formatQuotaWithCurrency } from '@/lib/currency'
import { formatTimestamp } from '@/lib/format'

import { getAbnormalChargeRecords, getAbnormalChargeUsers } from './api'
import type {
  AbnormalChargeCase,
  AbnormalChargeRecord,
  AbnormalChargeRecordStatus,
} from './types'

const CASE_LABELS: Record<AbnormalChargeCase, string> = {
  client_before_response: 'Client left before upstream responded',
  client_during_response: 'Client left while receiving',
  upstream_during_response: 'Upstream or gateway interrupted',
  upstream_before_response: 'Upstream or gateway failed',
}

/** A user picked from the list, or text matched against usernames and IDs. */
export type RecordsUserFilter =
  | { userId: number; username: string }
  | { keyword: string }

export type RecordSelection = Map<number, AbnormalChargeRecord>

type AbnormalRecordsPanelProps = {
  days: number
  userFilter: RecordsUserFilter
  onUserFilterChange: (filter: RecordsUserFilter) => void
  selected: RecordSelection
  onSelectedChange: (selected: RecordSelection) => void
  onRecover: (
    user: { user_id: number; username: string },
    records: AbnormalChargeRecord[]
  ) => void
}

function isRecoverable(record: AbnormalChargeRecord) {
  return record.user_caused && record.recovery_id === ''
}

export function AbnormalRecordsPanel(props: AbnormalRecordsPanelProps) {
  const { t } = useTranslation()
  const [status, setStatus] = useState<AbnormalChargeRecordStatus>('pending')
  const [endedHow, setEndedHow] = useState<AbnormalChargeCase | ''>('')
  const [pagination, setPagination] = useState({ pageIndex: 0, pageSize: 20 })
  const pinned = 'userId' in props.userFilter ? props.userFilter : null
  const typed = 'keyword' in props.userFilter ? props.userFilter.keyword : ''
  const keyword = useDebounce(typed.trim(), 400)

  const usersQuery = useQuery({
    queryKey: ['abnormal-charge-users', props.days, '', 1, 100],
    queryFn: ({ signal }) =>
      getAbnormalChargeUsers(
        { days: props.days, keyword: '', p: 1, page_size: 100 },
        signal
      ),
  })
  const userOptions: ComboboxInputOption[] = (usersQuery.data?.items ?? []).map(
    (user) => ({
      value: String(user.user_id),
      label: `${user.username || '-'} #${user.user_id}`,
      description: user.flagged ? t('Suspicious') : undefined,
    })
  )
  if (pinned && !userOptions.some((o) => o.value === String(pinned.userId))) {
    userOptions.unshift({
      value: String(pinned.userId),
      label: `${pinned.username || '-'} #${pinned.userId}`,
    })
  }

  const query = useQuery({
    queryKey: [
      'abnormal-charge-records',
      props.days,
      pinned?.userId ?? 0,
      keyword,
      status,
      endedHow,
      pagination.pageIndex,
      pagination.pageSize,
    ],
    queryFn: ({ signal }) =>
      getAbnormalChargeRecords(
        {
          days: props.days,
          user_id: pinned?.userId,
          keyword: pinned ? undefined : keyword,
          status,
          case: endedHow,
          p: pagination.pageIndex + 1,
          page_size: pagination.pageSize,
        },
        signal
      ),
  })
  const records = query.data?.items ?? []
  const { table } = useDataTable({
    data: records,
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

  const firstPage = () =>
    setPagination((previous) => ({ ...previous, pageIndex: 0 }))
  const recoverable = pinned ? records.filter(isRecoverable) : []
  const allOnPageSelected =
    recoverable.length > 0 && recoverable.every((r) => props.selected.has(r.id))
  const selectedQuota = [...props.selected.values()].reduce(
    (sum, record) => sum + record.forgone_quota,
    0
  )

  function toggle(records: AbnormalChargeRecord[], checked: boolean) {
    const next = new Map(props.selected)
    for (const record of records) {
      if (checked) next.set(record.id, record)
      else next.delete(record.id)
    }
    props.onSelectedChange(next)
  }

  const columns: StaticDataTableColumn<AbnormalChargeRecord>[] = [
    ...(pinned
      ? [
          {
            id: 'select',
            header: (
              <Checkbox
                aria-label={t('Select all recoverable records on this page')}
                checked={allOnPageSelected}
                disabled={recoverable.length === 0}
                onCheckedChange={(checked) =>
                  toggle(recoverable, checked === true)
                }
              />
            ),
            className: 'w-10',
            cell: (record: AbnormalChargeRecord) =>
              isRecoverable(record) ? (
                <Checkbox
                  aria-label={t('Select record {{id}}', { id: record.id })}
                  checked={props.selected.has(record.id)}
                  onCheckedChange={(checked) =>
                    toggle([record], checked === true)
                  }
                />
              ) : null,
          },
        ]
      : []),
    {
      id: 'time',
      header: t('Time'),
      cell: (record) => formatTimestamp(record.created_at),
    },
    {
      id: 'user',
      header: t('User'),
      cell: (record) => (
        <Button
          variant='link'
          size='sm'
          className='h-auto p-0'
          aria-label={t('Show only the records of {{username}}', {
            username: record.username || record.user_id,
          })}
          onClick={() => {
            props.onUserFilterChange({
              userId: record.user_id,
              username: record.username,
            })
            firstPage()
          }}
        >
          {record.username || '-'}
          <span className='text-muted-foreground text-xs'>
            #{record.user_id}
          </span>
        </Button>
      ),
    },
    {
      id: 'model',
      header: t('Model'),
      cell: (record) => (
        <div className='flex flex-col gap-0.5'>
          <span>{record.model_name}</span>
          <span className='text-muted-foreground text-xs'>
            {record.token_name}
            {record.ip ? ` · ${record.ip}` : ''}
          </span>
        </div>
      ),
    },
    {
      id: 'case',
      header: t('How it ended'),
      cell: (record) => (
        <div className='flex flex-col gap-0.5'>
          <span>{t(CASE_LABELS[record.case] ?? record.case)}</span>
          <span className='text-muted-foreground text-xs'>
            {t('{{seconds}}s after sending', {
              seconds: (record.elapsed_ms / 1000).toFixed(1),
            })}
            {record.client_received_data ? ` · ${t('Received output')}` : ''}
            {record.is_task ? ` · ${t('Task')}` : ''}
          </span>
        </div>
      ),
    },
    {
      id: 'upstream',
      header: t('Upstream'),
      cell: (record) => {
        if (record.upstream_billing === 'billed') {
          return (
            <div className='flex flex-col gap-0.5'>
              <Badge variant='destructive'>{t('Upstream charged')}</Badge>
              <span className='text-muted-foreground text-xs'>
                {t('{{amount}} upstream, {{charged}} paid', {
                  amount: formatQuotaWithCurrency(record.upstream_quota),
                  charged: formatQuotaWithCurrency(record.charged_quota),
                })}
              </span>
            </div>
          )
        }
        if (record.upstream_billing === 'unknown') {
          return <Badge variant='outline'>{t('Upstream unknown')}</Badge>
        }
        return <span className='text-muted-foreground'>-</span>
      },
    },
    {
      id: 'amount',
      header: t('Uncollected'),
      cell: (record) => formatQuotaWithCurrency(record.forgone_quota),
    },
    {
      id: 'status',
      header: t('Status'),
      cell: (record) => {
        if (!record.user_caused) {
          return <Badge variant='outline'>{t('Not caused by the user')}</Badge>
        }
        if (record.recovery_id) {
          return <Badge variant='secondary'>{t('Recovered')}</Badge>
        }
        return <Badge variant='warning'>{t('Pending')}</Badge>
      },
    },
  ]

  return (
    <div className='flex min-w-0 flex-col gap-3'>
      <div className='flex flex-wrap items-center gap-2'>
        <div className='w-64'>
          <ComboboxInput
            aria-label={t('Filter by user')}
            placeholder={t('All users, or search by username or user ID')}
            emptyText={t('No user left a request uncharged in this period')}
            options={userOptions}
            value={pinned ? String(pinned.userId) : typed}
            allowCustomValue
            onValueChange={(value) => {
              const option = usersQuery.data?.items.find(
                (user) => String(user.user_id) === value
              )
              props.onUserFilterChange(
                option
                  ? { userId: option.user_id, username: option.username }
                  : { keyword: value }
              )
              firstPage()
            }}
          />
        </div>
        <NativeSelect
          aria-label={t('Record status')}
          value={status}
          onChange={(event) => {
            setStatus(event.target.value as AbnormalChargeRecordStatus)
            firstPage()
          }}
        >
          <NativeSelectOption value='pending'>
            {t('Pending')}
          </NativeSelectOption>
          <NativeSelectOption value='recovered'>
            {t('Recovered')}
          </NativeSelectOption>
          <NativeSelectOption value=''>{t('All statuses')}</NativeSelectOption>
        </NativeSelect>
        <NativeSelect
          aria-label={t('How it ended')}
          value={endedHow}
          onChange={(event) => {
            setEndedHow(event.target.value as AbnormalChargeCase | '')
            firstPage()
          }}
        >
          <NativeSelectOption value=''>{t('Any ending')}</NativeSelectOption>
          {Object.entries(CASE_LABELS).map(([value, label]) => (
            <NativeSelectOption key={value} value={value}>
              {t(label)}
            </NativeSelectOption>
          ))}
        </NativeSelect>
        {pinned || typed ? (
          <Button
            variant='ghost'
            size='sm'
            onClick={() => {
              props.onUserFilterChange({ keyword: '' })
              firstPage()
            }}
          >
            {t('Show all users')}
          </Button>
        ) : null}
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
          <AlertTitle>
            {t('Failed to load abnormal request records')}
          </AlertTitle>
        </Alert>
      ) : (
        <StaticDataTable
          columns={columns}
          data={records}
          getRowKey={(record) => record.id}
          emptyContent={
            query.isLoading ? t('Loading...') : t('No abnormal request records')
          }
        />
      )}

      <div className='flex flex-wrap items-center justify-between gap-3'>
        {pinned ? (
          <Button
            variant='destructive'
            disabled={props.selected.size === 0}
            onClick={() =>
              props.onRecover(
                { user_id: pinned.userId, username: pinned.username },
                [...props.selected.values()]
              )
            }
          >
            {t('Recover {{count}} selected ({{amount}})', {
              count: props.selected.size,
              amount: formatQuotaWithCurrency(selectedQuota),
            })}
          </Button>
        ) : (
          <p className='text-muted-foreground text-sm'>
            {t('Pick one user to choose records to recover.')}
          </p>
        )}
        <DataTablePagination table={table} />
      </div>
    </div>
  )
}
