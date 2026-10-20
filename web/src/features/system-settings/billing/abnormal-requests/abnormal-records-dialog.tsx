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
import { ChevronLeft, ChevronRight } from 'lucide-react'
import { useState } from 'react'
import { useTranslation } from 'react-i18next'

import {
  StaticDataTable,
  type StaticDataTableColumn,
} from '@/components/data-table'
import { Dialog } from '@/components/dialog'
import { Alert, AlertTitle } from '@/components/ui/alert'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import { Checkbox } from '@/components/ui/checkbox'
import { NativeSelect, NativeSelectOption } from '@/components/ui/native-select'
import { formatQuotaWithCurrency } from '@/lib/currency'
import { formatTimestamp } from '@/lib/format'

import { getUserAbnormalCharges } from './api'
import type { AbnormalChargeRecord, AbnormalChargeRecordStatus } from './types'

const CASE_LABELS: Record<string, string> = {
  client_before_response: 'Client left before upstream responded',
  client_during_response: 'Client left while receiving',
  upstream_during_response: 'Upstream or gateway interrupted',
  upstream_before_response: 'Upstream or gateway failed',
}

type AbnormalRecordsDialogProps = {
  open: boolean
  onOpenChange: (open: boolean) => void
  user: { id: number; username: string }
  days: number
  onRecover: (records: AbnormalChargeRecord[]) => void
}

function isRecoverable(record: AbnormalChargeRecord) {
  return record.user_caused && record.recovery_id === ''
}

export function AbnormalRecordsDialog(props: AbnormalRecordsDialogProps) {
  const { t } = useTranslation()
  const [status, setStatus] = useState<AbnormalChargeRecordStatus>('pending')
  const [pagination, setPagination] = useState({ page: 1, pageSize: 20 })
  const [selected, setSelected] = useState<Map<number, AbnormalChargeRecord>>(
    () => new Map()
  )
  const query = useQuery({
    queryKey: [
      'abnormal-charge-records',
      props.user.id,
      props.days,
      status,
      pagination.page,
      pagination.pageSize,
    ],
    enabled: props.open,
    queryFn: ({ signal }) =>
      getUserAbnormalCharges(
        props.user.id,
        {
          days: props.days,
          status,
          p: pagination.page,
          page_size: pagination.pageSize,
        },
        signal
      ),
  })
  const records = query.data?.items ?? []
  const total = query.data?.total ?? 0
  const totalPages = Math.max(1, Math.ceil(total / pagination.pageSize))
  const recoverable = records.filter(isRecoverable)
  const allOnPageSelected =
    recoverable.length > 0 && recoverable.every((r) => selected.has(r.id))
  const selectedQuota = [...selected.values()].reduce(
    (sum, record) => sum + record.forgone_quota,
    0
  )

  function toggle(record: AbnormalChargeRecord, checked: boolean) {
    setSelected((current) => {
      const next = new Map(current)
      if (checked) next.set(record.id, record)
      else next.delete(record.id)
      return next
    })
  }

  function togglePage(checked: boolean) {
    setSelected((current) => {
      const next = new Map(current)
      for (const record of recoverable) {
        if (checked) next.set(record.id, record)
        else next.delete(record.id)
      }
      return next
    })
  }

  const columns: StaticDataTableColumn<AbnormalChargeRecord>[] = [
    {
      id: 'select',
      header: (
        <Checkbox
          aria-label={t('Select all recoverable records on this page')}
          checked={allOnPageSelected}
          disabled={recoverable.length === 0}
          onCheckedChange={(checked) => togglePage(checked === true)}
        />
      ),
      className: 'w-10',
      cell: (record) =>
        isRecoverable(record) ? (
          <Checkbox
            aria-label={t('Select record {{id}}', { id: record.id })}
            checked={selected.has(record.id)}
            onCheckedChange={(checked) => toggle(record, checked === true)}
          />
        ) : null,
    },
    {
      id: 'time',
      header: t('Time'),
      cell: (record) => formatTimestamp(record.created_at),
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
    <Dialog
      open={props.open}
      onOpenChange={props.onOpenChange}
      title={t('Abnormal request records')}
      description={`${props.user.username} (${t('User ID')}: ${props.user.id})`}
      contentClassName='sm:max-w-5xl'
      bodyClassName='min-h-48'
      footer={
        <div className='flex w-full flex-wrap items-center justify-between gap-3'>
          <div className='flex items-center gap-2'>
            <Button
              variant='outline'
              size='icon-sm'
              aria-label={t('Go to previous page')}
              disabled={pagination.page <= 1 || query.isFetching}
              onClick={() =>
                setPagination((current) => ({
                  ...current,
                  page: current.page - 1,
                }))
              }
            >
              <ChevronLeft />
            </Button>
            <span
              className='text-muted-foreground text-sm tabular-nums'
              aria-live='polite'
            >
              {t('Page {{current}} of {{total}}', {
                current: pagination.page,
                total: totalPages,
              })}
            </span>
            <Button
              variant='outline'
              size='icon-sm'
              aria-label={t('Go to next page')}
              disabled={
                pagination.page >= totalPages ||
                query.isFetching ||
                query.isError
              }
              onClick={() =>
                setPagination((current) => ({
                  ...current,
                  page: current.page + 1,
                }))
              }
            >
              <ChevronRight />
            </Button>
          </div>
          <Button
            variant='destructive'
            disabled={selected.size === 0}
            onClick={() => props.onRecover([...selected.values()])}
          >
            {t('Recover {{count}} selected ({{amount}})', {
              count: selected.size,
              amount: formatQuotaWithCurrency(selectedQuota),
            })}
          </Button>
        </div>
      }
    >
      <div className='flex min-w-0 flex-col gap-3'>
        <NativeSelect
          aria-label={t('Record status')}
          value={status}
          onChange={(event) => {
            setStatus(event.target.value as AbnormalChargeRecordStatus)
            setPagination((current) => ({ ...current, page: 1 }))
          }}
          className='w-48'
        >
          <NativeSelectOption value='pending'>
            {t('Pending')}
          </NativeSelectOption>
          <NativeSelectOption value='recovered'>
            {t('Recovered')}
          </NativeSelectOption>
          <NativeSelectOption value=''>{t('All')}</NativeSelectOption>
        </NativeSelect>
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
              query.isLoading
                ? t('Loading...')
                : t('No abnormal request records')
            }
          />
        )}
      </div>
    </Dialog>
  )
}
