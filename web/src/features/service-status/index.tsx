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
import { Activity } from 'lucide-react'
import { useTranslation } from 'react-i18next'

import { EmptyState } from '@/components/empty-state'
import { ErrorState } from '@/components/error-state'
import { PublicLayout } from '@/components/layout'
import { PageTransition } from '@/components/page-transition'
import { Skeleton } from '@/components/ui/skeleton'
import { getSuccessRateDotClass } from '@/features/performance-metrics/lib/format'
import dayjs from '@/lib/dayjs'
import { requireServerSuccess } from '@/lib/server-error-message'
import { cn } from '@/lib/utils'

import { getLiveServiceStatus } from './api'
import { StatusGroupCard } from './components/status-group-card'

const REFRESH_INTERVAL_MS = 30_000

export function ServiceStatus() {
  const { t } = useTranslation()
  const statusQuery = useQuery({
    queryKey: ['service-status'],
    queryFn: async () => requireServerSuccess(await getLiveServiceStatus()),
    refetchInterval: REFRESH_INTERVAL_MS,
  })
  const data = statusQuery.data?.data

  let content
  if (statusQuery.isLoading) {
    content = (
      <div className='flex flex-col gap-4'>
        <Skeleton className='h-48 w-full rounded-xl' />
        <Skeleton className='h-48 w-full rounded-xl' />
      </div>
    )
  } else if (!data) {
    content = (
      <ErrorState
        title={t('Unable to load service status')}
        description={
          statusQuery.error instanceof Error
            ? statusQuery.error.message
            : undefined
        }
        onRetry={() => void statusQuery.refetch()}
      />
    )
  } else if (data.groups.length === 0) {
    content = (
      <EmptyState
        icon={Activity}
        bordered
        title={t('No requests in the last hour')}
      />
    )
  } else {
    content = (
      <div className='flex flex-col gap-4'>
        {data.groups.map((group) => (
          <StatusGroupCard
            key={group.group}
            group={group}
            windowStart={data.window_start}
            windowEnd={data.window_end}
            bucketSeconds={data.bucket_seconds}
            shortWindowMinutes={data.short_window_minutes}
          />
        ))}
      </div>
    )
  }

  return (
    <PublicLayout showMainContainer={false}>
      <PageTransition className='mx-auto flex w-full max-w-[1280px] flex-col gap-6 px-3 pt-16 pb-10 sm:px-6 sm:pt-20 sm:pb-12 xl:px-8'>
        <header className='flex flex-col gap-3'>
          <h1 className='text-2xl font-semibold tracking-tight sm:text-3xl'>
            {t('Service Status')}
          </h1>
          <p className='text-muted-foreground max-w-3xl text-sm'>
            {t(
              'Success rates of requests from all users, by group and model, for each minute of the last hour. Failures caused by the request itself, such as invalid parameters, insufficient balance, rate limits or content moderation, are not counted.'
            )}
          </p>
          <div className='text-muted-foreground flex flex-wrap items-center gap-x-5 gap-y-2 text-xs'>
            {statusQuery.dataUpdatedAt > 0 && (
              <span>
                {t('Updated at {{time}}', {
                  time: dayjs(statusQuery.dataUpdatedAt).format('HH:mm:ss'),
                })}
              </span>
            )}
            <StatusLegend />
          </div>
        </header>
        {content}
      </PageTransition>
    </PublicLayout>
  )
}

function StatusLegend() {
  const { t } = useTranslation()
  const items = [
    { className: getSuccessRateDotClass(100), label: '100%' },
    { className: getSuccessRateDotClass(90), label: t('90% or more') },
    { className: getSuccessRateDotClass(70), label: t('70% or more') },
    { className: getSuccessRateDotClass(0), label: t('Below 70%') },
    { className: 'bg-muted-foreground/30', label: t('Too few requests') },
    { className: 'bg-muted', label: t('No requests') },
  ]
  return (
    <ul className='flex flex-wrap items-center gap-x-3 gap-y-1'>
      {items.map((item) => (
        <li key={item.label} className='flex items-center gap-1.5'>
          <span
            aria-hidden
            className={cn('size-2.5 rounded-[2px]', item.className)}
          />
          {item.label}
        </li>
      ))}
    </ul>
  )
}
