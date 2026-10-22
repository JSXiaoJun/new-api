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
import { Link } from '@tanstack/react-router'
import { useTranslation } from 'react-i18next'

import {
  StaticDataTable,
  staticDataTableClassNames,
  type StaticDataTableColumn,
} from '@/components/data-table'
import { EmptyState } from '@/components/empty-state'
import { LoadingState } from '@/components/loading-state'
import { TableId } from '@/components/table-id'
import { Button } from '@/components/ui/button'
import { toIntlLocale } from '@/i18n/languages'
import { formatLogQuota, formatNumber, formatPercent } from '@/lib/format'

import { getNetQuotaShare } from '../lib/rank-summary'
import type { ChannelQuotaRank } from '../types'

type ChannelRankTableProps = {
  rows: ChannelQuotaRank[]
  loading: boolean
  totalNetQuota: number
  range: { start: number; end: number }
}

export function ChannelRankTable(props: ChannelRankTableProps) {
  const { t, i18n } = useTranslation()
  const locale = toIntlLocale(i18n.resolvedLanguage || i18n.language)
  const numeric = staticDataTableClassNames.compactNumericCell

  const channelLabel = (row: ChannelQuotaRank) => {
    if (row.channel_id === 0) return t('No channel')
    return row.channel_name || t('Deleted channel')
  }

  const columns: StaticDataTableColumn<ChannelQuotaRank>[] = [
    {
      id: 'rank',
      header: t('Rank'),
      className: 'w-16',
      cell: (_row, index) => <TableId value={index + 1} />,
    },
    {
      id: 'channel',
      header: t('Channel'),
      cell: (row) => (
        <div className='flex min-w-0 items-center gap-2'>
          <span className='truncate font-medium'>{channelLabel(row)}</span>
          {row.channel_id !== 0 && <TableId value={`#${row.channel_id}`} />}
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
      id: 'prompt_tokens',
      header: t('Input tokens'),
      className: 'text-right',
      cellClassName: numeric,
      cell: (row) => formatNumber(row.prompt_tokens, locale),
    },
    {
      id: 'completion_tokens',
      header: t('Output tokens'),
      className: 'text-right',
      cellClassName: numeric,
      cell: (row) => formatNumber(row.completion_tokens, locale),
    },
    {
      id: 'consumed',
      header: t('Consumed'),
      className: 'text-right',
      cellClassName: numeric,
      cell: (row) => formatLogQuota(row.consumed_quota),
    },
    {
      id: 'refunded',
      header: t('Refunded'),
      className: 'text-right',
      cellClassName: numeric,
      cell: (row) => formatLogQuota(row.refunded_quota),
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
        formatPercent(getNetQuotaShare(row.net_quota, props.totalNetQuota)),
    },
    {
      id: 'actions',
      header: <span className='sr-only'>{t('Actions')}</span>,
      className: 'w-24 text-right',
      cellClassName: 'text-right',
      cell: (row) => (
        <Button
          variant='ghost'
          size='sm'
          render={
            <Link
              to='/usage-logs/$section'
              params={{ section: 'common' }}
              search={{
                page: 1,
                channel: String(row.channel_id),
                startTime: props.range.start * 1000,
                endTime: props.range.end * 1000 - 1000,
              }}
            />
          }
        >
          {t('View logs')}
        </Button>
      ),
    },
  ]

  return (
    <StaticDataTable
      tableClassName='min-w-[960px]'
      data={props.loading ? [] : props.rows}
      getRowKey={(row) => row.channel_id}
      columns={columns}
      emptyContent={
        props.loading ? (
          <LoadingState />
        ) : (
          <EmptyState
            title={t('No channel consumption on this day')}
            bordered={false}
          />
        )
      }
    />
  )
}
