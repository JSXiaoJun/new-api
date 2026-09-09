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
import { useTranslation } from 'react-i18next'

import { StaticDataTable } from '@/components/data-table'
import { Badge } from '@/components/ui/badge'
import { Empty, EmptyHeader, EmptyTitle } from '@/components/ui/empty'
import {
  parseLogOther,
  renderAuditContent,
} from '@/features/usage-logs/lib/format'
import { formatTimestampToDate } from '@/lib/format'

import type { UserQuotaCredit, UserQuotaLog } from '../../types'

function CreditSource(props: { source: string }) {
  const { t } = useTranslation()
  switch (props.source) {
    case 'topup':
      return t('Recharge')
    case 'redemption':
      return t('Redemption credit')
    case 'checkin':
      return t('Check-in reward')
    case 'registration':
      return t('Registration credit')
    case 'initial_quota':
      return t('Initial quota credit')
    case 'invitation_reward':
      return t('Invitee reward')
    case 'invitation_transfer':
      return t('Affiliate transfer to wallet')
    case 'admin_add':
      return t('Administrator credit')
    case 'admin_override':
      return t('Administrator balance override')
    case 'wallet_settlement_refund':
      return t('Settlement refund')
    case 'wallet_refund':
      return t('Request refund')
    case 'preconsume_rollback':
      return t('Reservation rollback')
    case 'task_refund':
      return t('Task refund')
    case 'task_settlement_refund':
      return t('Task settlement refund')
    case 'midjourney_refund':
      return t('Midjourney refund')
    case 'wallet_credit':
      return t('Quota adjustment')
    default:
      return props.source || '-'
  }
}

function NoQuotaRecords() {
  const { t } = useTranslation()
  return (
    <Empty>
      <EmptyHeader>
        <EmptyTitle>{t('No quota records found')}</EmptyTitle>
      </EmptyHeader>
    </Empty>
  )
}

export function UserQuotaCreditsTable(props: {
  records: UserQuotaCredit[]
  busy: boolean
}) {
  const { t } = useTranslation()
  return (
    <StaticDataTable<UserQuotaCredit>
      className='rounded-none border-0'
      tableClassName='min-w-[760px] table-fixed [&_th]:whitespace-normal'
      tableProps={{
        'aria-label': t('Credit records'),
        'aria-busy': props.busy,
      }}
      data={props.records}
      getRowKey={(record) => record.id}
      emptyContent={<NoQuotaRecords />}
      columns={[
        {
          id: 'time',
          header: t('Time'),
          className: 'w-40',
          cell: (record) => formatTimestampToDate(record.created_at),
        },
        {
          id: 'delta',
          header: t('Credit (raw quota)'),
          className: 'w-36',
          cellClassName: 'font-mono tabular-nums whitespace-normal break-all',
          cell: (record) => `+${record.delta}`,
        },
        {
          id: 'source',
          header: t('Source'),
          className: 'w-36',
          cellClassName:
            'whitespace-normal break-words [overflow-wrap:anywhere]',
          cell: (record) => <CreditSource source={record.source} />,
        },
        {
          id: 'reference',
          header: t('Reference'),
          cellClassName: 'whitespace-normal break-all',
          cell: (record) => (
            <div className='space-y-1'>
              {record.reference ? <div>{record.reference}</div> : null}
              {record.request_id ? (
                <div>
                  <span className='text-muted-foreground'>
                    {t('Request ID')}:{' '}
                  </span>
                  <span>{record.request_id}</span>
                </div>
              ) : null}
              {!record.reference && !record.request_id ? '-' : null}
            </div>
          ),
        },
        {
          id: 'operator',
          header: t('Operator ID'),
          className: 'w-28',
          cell: (record) => record.operator_id || '-',
        },
      ]}
    />
  )
}

export function UserQuotaLegacyTable(props: {
  records: UserQuotaLog[]
  busy: boolean
}) {
  const { t } = useTranslation()
  return (
    <StaticDataTable<UserQuotaLog>
      className='rounded-none border-0'
      tableClassName='min-w-[760px] table-fixed [&_th]:whitespace-normal'
      tableProps={{
        'aria-label': t('Historical logs'),
        'aria-busy': props.busy,
      }}
      data={props.records}
      getRowKey={(record) => record.id}
      emptyContent={<NoQuotaRecords />}
      columns={[
        {
          id: 'time',
          header: t('Time'),
          className: 'w-40',
          cell: (record) => formatTimestampToDate(record.created_at),
        },
        {
          id: 'type',
          header: t('Type'),
          className: 'w-32',
          cellClassName: 'whitespace-normal',
          cell: (record) => {
            if (record.type === 2 && record.quota < 0) {
              return (
                <Badge
                  variant='destructive'
                  className='h-auto min-h-5 rounded-sm whitespace-normal'
                >
                  {t('Negative consumption (anomaly)')}
                </Badge>
              )
            }
            if (record.type === 1) return t('Recharge / subscription')
            if (record.type === 3) return t('Manage')
            if (record.type === 6) return t('Refund')
            return t('System')
          },
        },
        {
          id: 'quota',
          header: t('Recorded quota (raw)'),
          className: 'w-32',
          cellClassName: 'whitespace-normal break-all tabular-nums',
          cell: (record) => {
            if (
              record.quota !== 0 &&
              (record.type === 2 || record.type === 6)
            ) {
              return String(record.quota)
            }
            return t('Unknown amount')
          },
        },
        {
          id: 'details',
          header: t('Details'),
          cellClassName:
            'whitespace-normal break-words [overflow-wrap:anywhere]',
          cell: (record) =>
            renderAuditContent(parseLogOther(record.other), t) ||
            record.content ||
            '-',
        },
        {
          id: 'operator',
          header: t('Operator Admin'),
          className: 'w-32',
          cellClassName: 'whitespace-normal break-all',
          cell: (record) => {
            const admin = parseLogOther(record.other)?.admin_info
            if (admin?.admin_username) return admin.admin_username
            if (admin?.admin_id) return `ID: ${admin.admin_id}`
            return '-'
          },
        },
      ]}
    />
  )
}
