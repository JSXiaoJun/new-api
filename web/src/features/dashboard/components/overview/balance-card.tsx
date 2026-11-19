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
import { ArrowRight, Flame, ShieldCheck, TrendingDown } from 'lucide-react'
import { useMemo } from 'react'
import { useTranslation } from 'react-i18next'

import { Button } from '@/components/ui/button'
import { getUserQuotaDates } from '@/features/dashboard/api'
import { formatNumber, formatQuota } from '@/lib/format'
import { requireServerSuccess } from '@/lib/server-error-message'
import { computeTimeRange } from '@/lib/time'
import { cn } from '@/lib/utils'
import { useAuthStore } from '@/stores/auth-store'

function getRunwayDays(
  remainQuota: number,
  recentUsage: number
): number | null {
  if (remainQuota <= 0 || recentUsage <= 0) return null
  const days = remainQuota / recentUsage
  if (!Number.isFinite(days)) return null
  return days
}

type HealthLevel = 'healthy' | 'caution' | 'critical'

function getHealthLevel(remainQuota: number, recentUsage: number): HealthLevel {
  if (remainQuota <= 0) return 'critical'
  const days = getRunwayDays(remainQuota, recentUsage)
  if (days !== null && days < 3) return 'caution'
  return 'healthy'
}

const HEALTH_CONFIG: Record<
  HealthLevel,
  { dotClass: string; labelKey: string }
> = {
  healthy: {
    dotClass: 'bg-success',
    labelKey: 'Healthy',
  },
  caution: {
    dotClass: 'bg-warning',
    labelKey: 'Low balance',
  },
  critical: {
    dotClass: 'bg-destructive',
    labelKey: 'Balance depleted',
  },
}

export function BalanceCard() {
  const { t } = useTranslation()
  const user = useAuthStore((state) => state.auth.user)

  const summaryTimeRange = useMemo(() => computeTimeRange(1), [])
  const remainQuota = Number(user?.quota ?? 0)

  const usageTrendQuery = useQuery({
    queryKey: [
      'dashboard',
      'overview',
      'summary-sparklines',
      summaryTimeRange.start_timestamp,
      summaryTimeRange.end_timestamp,
    ],
    queryFn: async () =>
      requireServerSuccess(
        await getUserQuotaDates({
          start_timestamp: summaryTimeRange.start_timestamp,
          end_timestamp: summaryTimeRange.end_timestamp,
          default_time: 'hour',
        })
      ),
    staleTime: 60 * 1000,
  })

  const recentUsage = useMemo(
    () =>
      (usageTrendQuery.data?.data ?? []).reduce(
        (total, item) => total + (Number(item.quota) || 0),
        0
      ),
    [usageTrendQuery.data?.data]
  )

  const healthLevel = getHealthLevel(remainQuota, recentUsage)
  const healthCfg = HEALTH_CONFIG[healthLevel]
  const runwayDays = getRunwayDays(remainQuota, recentUsage)

  let runwayDisplay: string
  if (runwayDays !== null) {
    if (runwayDays < 1) {
      runwayDisplay = t('Less than 1 day left')
    } else if (runwayDays > 999) {
      runwayDisplay = `999+ ${t('days')}`
    } else {
      runwayDisplay = `~${formatNumber(Math.floor(runwayDays))} ${t('days')}`
    }
  } else if (remainQuota <= 0) {
    runwayDisplay = t('Balance depleted')
  } else {
    runwayDisplay = t('No recent usage')
  }

  return (
    <div className='flex flex-col gap-3 overflow-hidden rounded-2xl border bg-[linear-gradient(135deg,color-mix(in_oklch,var(--overview-accent-2)_12%,var(--background))_0%,color-mix(in_oklch,oklch(0.82_0.04_155)_8%,var(--background))_48%,color-mix(in_oklch,var(--overview-accent-1)_7%,var(--background))_100%)] p-4 shadow-xs sm:gap-4 sm:p-5'>
      <div className='flex flex-col gap-2 sm:gap-3'>
        <div className='flex items-center justify-between'>
          <span className='text-muted-foreground text-xs font-medium'>
            {t('Credit remaining')}
          </span>
          <span className='flex items-center gap-1.5'>
            <span
              className={cn('size-1.5 rounded-full', healthCfg.dotClass)}
              aria-hidden='true'
            />
            <span className='text-muted-foreground text-[11px] font-medium'>
              {t(healthCfg.labelKey)}
            </span>
          </span>
        </div>

        <div className='font-mono text-xl font-semibold tracking-tight sm:text-2xl'>
          {formatQuota(remainQuota)}
        </div>

        <div className='grid grid-cols-2 gap-2'>
          <div className='bg-background/60 rounded-lg px-2.5 py-2'>
            <div className='text-muted-foreground flex items-center gap-1 text-[11px] leading-none font-medium'>
              <Flame className='size-3 shrink-0' aria-hidden='true' />
              <span className='truncate'>{t('Last 24h usage')}</span>
            </div>
            <div className='text-foreground mt-1.5 truncate text-xs font-semibold tabular-nums'>
              {formatQuota(recentUsage)}
            </div>
          </div>
          <div className='bg-background/60 rounded-lg px-2.5 py-2'>
            <div className='text-muted-foreground flex items-center gap-1 text-[11px] leading-none font-medium'>
              {runwayDays !== null && runwayDays < 3 ? (
                <TrendingDown className='size-3 shrink-0' aria-hidden='true' />
              ) : (
                <ShieldCheck className='size-3 shrink-0' aria-hidden='true' />
              )}
              <span className='truncate'>{t('Runway')}</span>
            </div>
            <div
              className={cn(
                'mt-1.5 truncate text-xs font-semibold tabular-nums',
                healthLevel === 'critical' && 'text-destructive',
                healthLevel === 'caution' && 'text-warning'
              )}
            >
              {runwayDisplay}
            </div>
          </div>
        </div>
      </div>

      <Button className='justify-between' render={<Link to='/wallet' />}>
        <span>{t('Wallet')}</span>
        <ArrowRight data-icon='inline-end' />
      </Button>
    </div>
  )
}
