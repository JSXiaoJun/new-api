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
import { ArrowDownUp, Coins, Radio, Undo2 } from 'lucide-react'
import { useMemo, useState } from 'react'
import { useTranslation } from 'react-i18next'

import { DatePicker } from '@/components/date-picker'
import { ErrorState } from '@/components/error-state'
import { SectionPageLayout } from '@/components/layout'
import { Button } from '@/components/ui/button'
import { StatCard } from '@/features/dashboard/components/ui/stat-card'
import { toIntlLocale } from '@/i18n/languages'
import { formatLogQuota, formatNumber } from '@/lib/format'

import { getChannelQuotaRank } from './api'
import { ChannelRankTable } from './components/channel-rank-table'
import { getDayRange, summarizeChannelRank } from './lib/rank-summary'
import type { ChannelQuotaRank } from './types'

const EMPTY_ROWS: ChannelQuotaRank[] = []

export function ChannelRank() {
  const { t, i18n } = useTranslation()
  const locale = toIntlLocale(i18n.resolvedLanguage || i18n.language)
  const [date, setDate] = useState<Date>(() => new Date())
  const range = useMemo(() => getDayRange(date), [date])

  const query = useQuery({
    queryKey: ['channel-quota-rank', range.start, range.end],
    queryFn: () => getChannelQuotaRank(range.start, range.end),
  })
  const rows = query.data ?? EMPTY_ROWS
  const summary = useMemo(() => summarizeChannelRank(rows), [rows])
  const loading = query.isLoading

  return (
    <SectionPageLayout>
      <SectionPageLayout.Title>
        {t('Channel Daily Rank')}
      </SectionPageLayout.Title>
      <SectionPageLayout.Actions>
        <DatePicker
          selected={date}
          onSelect={(next) => {
            if (next) setDate(next)
          }}
        />
        <Button
          variant='outline'
          onClick={() => query.refetch()}
          disabled={query.isFetching}
        >
          {t('Refresh')}
        </Button>
      </SectionPageLayout.Actions>
      <SectionPageLayout.Content>
        <div className='space-y-4'>
          <div className='grid gap-3 sm:grid-cols-2 xl:grid-cols-4'>
            <StatCard
              title={t('Net consumption')}
              value={formatLogQuota(summary.netQuota)}
              description={t('Consumption minus refunds for the day')}
              icon={Coins}
              tone='accent-1'
              loading={loading}
            />
            <StatCard
              title={t('Consumed')}
              value={formatLogQuota(summary.consumedQuota)}
              description={t('Total charged before refunds')}
              icon={ArrowDownUp}
              tone='accent-2'
              loading={loading}
            />
            <StatCard
              title={t('Refunded')}
              value={formatLogQuota(summary.refundedQuota)}
              description={t('Total returned by refund logs')}
              icon={Undo2}
              tone='accent-3'
              loading={loading}
            />
            <StatCard
              title={t('Requests')}
              value={formatNumber(summary.requestCount, locale)}
              description={t('{{count}} channels with usage', {
                count: summary.channelCount,
              })}
              icon={Radio}
              tone='accent-1'
              loading={loading}
            />
          </div>
          {query.isError ? (
            <ErrorState
              title={t('Failed to load channel rank')}
              onRetry={() => query.refetch()}
            />
          ) : (
            <ChannelRankTable
              rows={rows}
              loading={loading}
              totalNetQuota={summary.netQuota}
              range={range}
            />
          )}
        </div>
      </SectionPageLayout.Content>
    </SectionPageLayout>
  )
}
