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
import { CircleAlert } from 'lucide-react'
import { useTranslation } from 'react-i18next'

import {
  Tooltip,
  TooltipContent,
  TooltipProvider,
  TooltipTrigger,
} from '@/components/ui/tooltip'
import { useStatus } from '@/hooks/use-status'
import { formatUseTime } from '@/lib/format'
import { cn } from '@/lib/utils'

import {
  adjustFirstTokenDisplaySeconds,
  parseFirstTokenDisplayConfig,
} from '../lib/first-token-display'
import {
  durationSeverity,
  firstTokenSeverity,
  latencyBarClasses,
  latencyTextClasses,
} from '../lib/latency'
import type { LogOtherData } from '../types'
import { useShowRawTiming } from './usage-logs-provider'

interface TimingMetricsCellProps {
  useTimeSec: number
  frtMs?: number
  isStream: boolean
  className?: string
  /**
   * `bar` (default) draws a full-height color segment beside the labels,
   * matching the dense desktop table. `dot` swaps that segment for small
   * status dots inline with each label, matching the lighter-weight status
   * indicator used elsewhere on the mobile card.
   */
  indicator?: 'bar' | 'dot'
  compact?: boolean
}

export function TimingMetricsCell(props: TimingMetricsCellProps) {
  const { t } = useTranslation()
  const { status } = useStatus()
  const showRawTiming = useShowRawTiming()
  const firstTokenDisplayConfig = parseFirstTokenDisplayConfig(
    status?.first_token_display_rules
  )
  const indicator = props.indicator ?? 'bar'
  const showFirstToken = props.isStream
  let firstTokenSeconds: number | null = null
  if (props.frtMs != null && Number.isFinite(props.frtMs)) {
    if (showRawTiming) {
      firstTokenSeconds = props.frtMs > 0 ? props.frtMs / 1000 : null
    } else {
      firstTokenSeconds = adjustFirstTokenDisplaySeconds(
        props.frtMs / 1000,
        firstTokenDisplayConfig
      )
    }
  }
  const firstTokenLevel =
    firstTokenSeconds == null ? null : firstTokenSeverity(firstTokenSeconds)
  const firstTokenTextClass =
    firstTokenLevel == null
      ? 'text-muted-foreground'
      : latencyTextClasses[firstTokenLevel]
  const firstTokenDotClass =
    firstTokenLevel == null ? 'bg-neutral' : latencyBarClasses[firstTokenLevel]
  const totalTimeLevel = durationSeverity(props.useTimeSec)
  const splitBar = showFirstToken && firstTokenLevel != null
  const firstTokenLabel =
    firstTokenSeconds == null ? t('N/A') : formatUseTime(firstTokenSeconds)
  const totalTimeLabel = formatUseTime(props.useTimeSec)

  const labels = (
    <div
      className={cn(
        'flex min-h-8 min-w-0 flex-col justify-center gap-0.5 text-xs leading-tight',
        props.compact &&
          'min-h-0 flex-row flex-wrap items-center gap-x-2.5 gap-y-1'
      )}
    >
      {showFirstToken && (
        <div className='flex items-baseline gap-1.5'>
          {indicator === 'dot' && (
            <span
              aria-hidden
              className={cn(
                'size-1.5 shrink-0 rounded-full',
                firstTokenDotClass
              )}
            />
          )}
          <span className='text-muted-foreground shrink-0'>
            {t('First token')}
          </span>
          <span className={cn('font-medium tabular-nums', firstTokenTextClass)}>
            {firstTokenLabel}
          </span>
        </div>
      )}
      <div className='flex items-baseline gap-1.5'>
        {indicator === 'dot' && (
          <span
            aria-hidden
            className={cn(
              'size-1.5 shrink-0 rounded-full',
              latencyBarClasses[totalTimeLevel]
            )}
          />
        )}
        <span className='text-muted-foreground shrink-0'>{t('Duration')}</span>
        <span
          className={cn(
            'font-medium tabular-nums',
            latencyTextClasses[totalTimeLevel]
          )}
        >
          {totalTimeLabel}
        </span>
      </div>
    </div>
  )

  if (indicator === 'dot') {
    return (
      <div className={cn('flex items-stretch', props.className)}>{labels}</div>
    )
  }

  return (
    <div className={cn('flex items-stretch gap-2', props.className)}>
      <span
        aria-hidden
        className={cn(
          'flex w-1 shrink-0 flex-col overflow-hidden rounded-full',
          !splitBar && latencyBarClasses[totalTimeLevel]
        )}
      >
        {splitBar && (
          <>
            <span className={cn('flex-1', firstTokenDotClass)} />
            <span className={cn('flex-1', latencyBarClasses[totalTimeLevel])} />
          </>
        )}
      </span>
      {labels}
    </div>
  )
}

interface StreamTpsCellProps {
  isStream: boolean
  compact?: boolean
  /** Task logs are asynchronous jobs; stream vs non-stream does not apply. */
  isTask?: boolean
  /** The task request returned its result inline, so it is a synchronous call. */
  isSyncTask?: boolean
  tokensPerSecond?: number | null
  streamStatus?: LogOtherData['stream_status']
  className?: string
}

export function StreamTpsCell(props: StreamTpsCellProps) {
  const { t } = useTranslation()
  const showStreamError =
    props.isStream && props.streamStatus && props.streamStatus.status !== 'ok'
  const tpsLabel =
    props.tokensPerSecond != null
      ? `${Math.round(props.tokensPerSecond)} t/s`
      : '—'
  let streamLabel = props.isStream ? t('Stream') : t('Non-stream')
  if (props.isTask) {
    streamLabel = props.isSyncTask ? t('Sync') : t('Async')
  }

  return (
    <div
      className={cn(
        'flex shrink-0 flex-col items-start justify-center gap-0.5 text-xs leading-tight',
        props.compact && 'flex-row flex-wrap items-center gap-1.5',
        props.className
      )}
    >
      <span
        className={cn(
          'inline-flex items-center gap-1 font-medium',
          props.isStream ? 'text-info' : 'text-muted-foreground'
        )}
      >
        {streamLabel}
        {showStreamError && (
          <TooltipProvider>
            <Tooltip>
              <TooltipTrigger
                render={<CircleAlert className='text-destructive size-3' />}
              />
              <TooltipContent>
                <div className='space-y-0.5 text-xs'>
                  <p>
                    {t('Stream Status')}: {t('Error')}
                  </p>
                  <p>{props.streamStatus?.end_reason || 'unknown'}</p>
                  {(props.streamStatus?.error_count ?? 0) > 0 && (
                    <p>
                      {t('Soft Errors')}: {props.streamStatus?.error_count}
                    </p>
                  )}
                </div>
              </TooltipContent>
            </Tooltip>
          </TooltipProvider>
        )}
      </span>
      {(!props.compact ||
        (props.isStream && props.tokensPerSecond != null)) && (
        <span className='text-muted-foreground/60 px-0.5 tabular-nums'>
          {tpsLabel}
        </span>
      )}
    </div>
  )
}
