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
import { Link } from '@tanstack/react-router'
import { useState } from 'react'
import { useTranslation } from 'react-i18next'

import {
  StaticDataTable,
  staticDataTableClassNames,
  type StaticDataTableColumn,
} from '@/components/data-table'
import { Dialog } from '@/components/dialog'
import { EmptyState } from '@/components/empty-state'
import { ErrorState } from '@/components/error-state'
import { LoadingState } from '@/components/loading-state'
import { TableId } from '@/components/table-id'
import { Button } from '@/components/ui/button'
import { toIntlLocale } from '@/i18n/languages'
import { formatLogQuota, formatNumber, formatPercent } from '@/lib/format'

import { getChannelUserQuotaRank } from '../api'
import { getNetQuotaShare } from '../lib/rank-summary'
import type { ChannelUserQuotaRank } from '../types'

type ChannelUserShareDialogProps = {
  channelId: number
  channelLabel: string
  channelNetQuota: number
  range: { start: number; end: number }
}

export function ChannelUserShareDialog(props: ChannelUserShareDialogProps) {
  const { t, i18n } = useTranslation()
  const locale = toIntlLocale(i18n.resolvedLanguage || i18n.language)
  const numeric = staticDataTableClassNames.compactNumericCell
  const [open, setOpen] = useState(false)

  const query = useQuery({
    queryKey: [
      'channel-user-quota-rank',
      props.channelId,
      props.range.start,
      props.range.end,
    ],
    queryFn: () =>
      getChannelUserQuotaRank(
        props.channelId,
        props.range.start,
        props.range.end
      ),
    enabled: open,
  })

  const columns: StaticDataTableColumn<ChannelUserQuotaRank>[] = [
    {
      id: 'rank',
      header: t('Rank'),
      className: 'w-14',
      cell: (_row, index) => <TableId value={index + 1} />,
    },
    {
      id: 'user',
      header: t('User'),
      cell: (row) => (
        <div className='flex min-w-0 items-center gap-2'>
          <span className='truncate font-medium'>
            {row.username || t('Deleted user')}
          </span>
          <TableId value={`#${row.user_id}`} />
        </div>
      ),
    },
    {
      id: 'requests',
      header: t('Requests'),
      className: 'text-right',
      cellClassName: numeric,
      cell: (row) => formatNumber(row.request_count, locale),
    },
    {
      id: 'net',
      header: t('Net consumption'),
      className: 'text-right',
      cellClassName: `${numeric} font-semibold`,
      cell: (row) => formatLogQuota(row.net_quota),
    },
    {
      id: 'share',
      header: t('Share'),
      className: 'text-right',
      cellClassName: numeric,
      cell: (row) =>
        formatPercent(getNetQuotaShare(row.net_quota, props.channelNetQuota)),
    },
  ]

  return (
    <Dialog
      open={open}
      onOpenChange={setOpen}
      title={t('User consumption share')}
      description={t('Top 10 users by net consumption on {{channel}}', {
        channel: props.channelLabel,
      })}
      trigger={
        <Button variant='ghost' size='sm'>
          {t('View share')}
        </Button>
      }
      footer={
        <Button
          variant='outline'
          render={
            <Link
              to='/usage-logs/$section'
              params={{ section: 'common' }}
              search={{
                page: 1,
                channel: String(props.channelId),
                startTime: props.range.start * 1000,
                endTime: props.range.end * 1000 - 1000,
              }}
            />
          }
        >
          {t('View logs')}
        </Button>
      }
    >
      {query.isError ? (
        <ErrorState
          title={t('Failed to load user share')}
          onRetry={() => query.refetch()}
        />
      ) : (
        <StaticDataTable
          tableClassName='min-w-[520px]'
          data={query.isLoading ? [] : (query.data ?? [])}
          getRowKey={(row) => row.user_id}
          columns={columns}
          emptyContent={
            query.isLoading ? (
              <LoadingState />
            ) : (
              <EmptyState
                title={t('No user consumption on this channel')}
                bordered={false}
              />
            )
          }
        />
      )}
    </Dialog>
  )
}
