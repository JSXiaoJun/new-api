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

import { GroupBadge } from '@/components/group-badge'
import {
  Card,
  CardAction,
  CardContent,
  CardDescription,
  CardHeader,
  CardTitle,
} from '@/components/ui/card'

import { buildMinuteSlots } from '../lib/minutes'
import type { LiveGroupStatus } from '../types'
import { MinuteStrip } from './minute-strip'
import { SuccessRateValue } from './success-rate-value'

interface StatusGroupCardProps {
  group: LiveGroupStatus
  windowStart: number
  windowEnd: number
  bucketSeconds: number
  shortWindowMinutes: number
}

export function StatusGroupCard(props: StatusGroupCardProps) {
  const { t } = useTranslation()
  const shortLabel = t('Last {{minutes}} minutes', {
    minutes: props.shortWindowMinutes,
  })
  const windowLabel = t('Last hour')

  return (
    <Card>
      <CardHeader className='border-b'>
        <CardTitle className='flex min-w-0 items-center gap-2'>
          <GroupBadge group={props.group.group} />
        </CardTitle>
        {props.group.description && (
          <CardDescription>{t(props.group.description)}</CardDescription>
        )}
        <CardAction>
          <dl className='flex items-start gap-5 text-right'>
            <div>
              <dt className='text-muted-foreground text-xs'>{shortLabel}</dt>
              <dd className='mt-0.5 text-base'>
                <SuccessRateValue rate={props.group.short_success_rate} />
              </dd>
            </div>
            <div>
              <dt className='text-muted-foreground text-xs'>{windowLabel}</dt>
              <dd className='mt-0.5 text-base'>
                <SuccessRateValue rate={props.group.window_success_rate} />
              </dd>
            </div>
          </dl>
        </CardAction>
      </CardHeader>
      <CardContent>
        <ul className='flex flex-col gap-3'>
          {props.group.models.map((model) => (
            <li
              key={model.model_name}
              className='grid grid-cols-[minmax(0,1fr)_auto] items-center gap-x-4 gap-y-1.5 md:grid-cols-[minmax(0,14rem)_minmax(0,1fr)_auto]'
            >
              <span
                className='truncate font-mono text-sm'
                title={model.model_name}
              >
                {model.model_name}
              </span>
              <MinuteStrip
                className='col-span-2 row-start-2 md:col-span-1 md:row-start-auto'
                label={t(
                  'Success rate of {{model}} per minute over the last hour',
                  {
                    model: model.model_name,
                  }
                )}
                slots={buildMinuteSlots(
                  props.windowStart,
                  props.windowEnd,
                  props.bucketSeconds,
                  model.minutes
                )}
              />
              <dl className='flex items-center gap-3 text-xs'>
                <div className='flex items-baseline gap-1' title={shortLabel}>
                  <dt className='text-muted-foreground'>
                    {t('{{minutes}} min', {
                      minutes: props.shortWindowMinutes,
                    })}
                  </dt>
                  <dd>
                    <SuccessRateValue rate={model.short_success_rate} />
                  </dd>
                </div>
                <div className='flex items-baseline gap-1' title={windowLabel}>
                  <dt className='text-muted-foreground'>{t('1 h')}</dt>
                  <dd>
                    <SuccessRateValue rate={model.window_success_rate} />
                  </dd>
                </div>
              </dl>
            </li>
          ))}
        </ul>
      </CardContent>
    </Card>
  )
}
