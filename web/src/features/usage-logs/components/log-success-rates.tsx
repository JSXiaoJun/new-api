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
/* eslint-disable react-refresh/only-export-components */
import { useQuery } from '@tanstack/react-query'
import { createContext, useContext, useMemo, type ReactNode } from 'react'

import { requireServerSuccess } from '@/lib/server-error-message'

import { getLogSuccessRates } from '../api'
import type { UsageLog } from '../data/schema'
import {
  collectSuccessRatePoints,
  type PlatformSuccessRate,
} from '../lib/success-rate'

type LogSuccessRates = ReadonlyMap<number, PlatformSuccessRate>

const EMPTY_RATES: LogSuccessRates = new Map()

const LogSuccessRatesContext = createContext<LogSuccessRates>(EMPTY_RATES)

/**
 * Loads the platform success rate around each failed log on the page, keyed
 * by log id, in one request.
 */
export function useLogSuccessRatesQuery(
  logs: UsageLog[],
  enabled: boolean
): LogSuccessRates {
  const request = useMemo(
    () =>
      enabled
        ? collectSuccessRatePoints(logs, Math.floor(Date.now() / 1000))
        : { ids: [], points: [] },
    [logs, enabled]
  )
  const query = useQuery({
    queryKey: ['log-success-rates', request.points],
    queryFn: async () =>
      requireServerSuccess(await getLogSuccessRates(request.points)).data,
    enabled: request.points.length > 0,
    staleTime: 60 * 1000,
    meta: { errorToast: false },
  })

  return useMemo(() => {
    if (!query.data) return EMPTY_RATES
    const windowMinutes = query.data.window_minutes
    const rates = new Map<number, PlatformSuccessRate>()
    request.ids.forEach((id, index) => {
      const rate = query.data.success_rates[index]
      if (rate != null) rates.set(id, { rate, windowMinutes })
    })
    return rates
  }, [query.data, request.ids])
}

export function LogSuccessRatesProvider(props: {
  rates: LogSuccessRates
  children: ReactNode
}) {
  return (
    <LogSuccessRatesContext.Provider value={props.rates}>
      {props.children}
    </LogSuccessRatesContext.Provider>
  )
}

/** Platform success rate around a failed log, or null when none is known. */
export function useLogSuccessRate(logId: number): PlatformSuccessRate | null {
  return useContext(LogSuccessRatesContext).get(logId) ?? null
}
