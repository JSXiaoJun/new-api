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
import type { TFunction } from 'i18next'

import type { LogOtherData } from '../types'

export type RelayTermination = NonNullable<LogOtherData['termination']>

/**
 * Who ended an abnormal upstream attempt, as recorded by the gateway.
 * Unknown values from newer backends fall back to the raw value.
 */
export function getTerminationEndedByLabel(
  endedBy: string | undefined,
  t: TFunction
): string {
  switch (endedBy) {
    case 'client':
      return t('Client disconnected')
    case 'upstream':
      return t('Upstream disconnected')
    case 'gateway':
      return t('Gateway interrupted')
    default:
      return endedBy || t('Unknown')
  }
}

export function getTerminationPhaseLabel(
  phase: string | undefined,
  t: TFunction
): string {
  switch (phase) {
    case 'sending':
      return t('Sending request to upstream')
    case 'awaiting_response':
      return t('Waiting for upstream response')
    case 'receiving':
      return t('Receiving upstream response')
    default:
      return phase || t('Unknown')
  }
}

export function getTerminationReasonLabel(
  reason: string | undefined,
  t: TFunction
): string {
  switch (reason) {
    case 'canceled':
    case 'client_gone':
      return t('Client closed the connection')
    case 'ping_fail':
      return t('Keepalive write to client failed')
    case 'timeout':
      return t('Gateway request timeout')
    case 'stream_timeout':
      return t('Upstream stream idle timeout')
    case 'connection_error':
      return t('Upstream connection broken')
    case 'response_failed':
      return t('Upstream returned a failure event')
    case 'missing_terminal':
      return t('Upstream stream ended without a completion event')
    case 'line_too_long':
      return t('Upstream stream line exceeded the buffer')
    case 'panic':
      return t('Gateway internal error')
    default:
      return reason || t('Unknown')
  }
}

export function terminationTextClass(endedBy: string | undefined): string {
  return endedBy === 'client'
    ? 'text-amber-600 dark:text-amber-400'
    : 'text-red-600 dark:text-red-400'
}
