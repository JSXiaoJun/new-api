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
import { Unplug } from 'lucide-react'
import { useTranslation } from 'react-i18next'

import { formatUseTime } from '@/lib/format'
import { cn } from '@/lib/utils'

import {
  getTerminationEndedByLabel,
  getTerminationPhaseLabel,
  getTerminationReasonLabel,
  terminationTextClass,
  type RelayTermination,
} from '../lib/termination'
import { DetailRow, DetailSection } from './dialogs/log-detail-layout'

interface TerminationDetailsProps {
  /** Absent when only a safety policy block was recorded (e.g. error logs). */
  termination?: RelayTermination
  /** Upstream safety policy code, which names the block in place of the side. */
  policyBlock?: string
  /** Masked transport error; only passed for admins. */
  transportError?: string
}

export function TerminationDetails(props: TerminationDetailsProps) {
  const { t } = useTranslation()
  const termination = props.termination
  const elapsedMs = termination?.elapsed_ms
  const endedByLabel = props.policyBlock
    ? t('Blocked by safety policy')
    : getTerminationEndedByLabel(termination?.ended_by, t)
  const endedByClass = props.policyBlock
    ? terminationTextClass('upstream')
    : terminationTextClass(termination?.ended_by)
  return (
    <DetailSection
      icon={<Unplug className='size-3.5' aria-hidden='true' />}
      label={t('Connection Termination')}
      variant={
        termination?.ended_by === 'client' && !props.policyBlock
          ? 'default'
          : 'danger'
      }
    >
      <DetailRow
        label={t('Ended By')}
        value={
          <span className={cn('font-medium', endedByClass)}>
            {endedByLabel}
          </span>
        }
      />
      {props.policyBlock && (
        <DetailRow label={t('Policy code')} value={props.policyBlock} mono />
      )}
      {termination && (
        <>
          <DetailRow
            label={t('Phase')}
            value={getTerminationPhaseLabel(termination.phase, t)}
          />
          <DetailRow
            label={t('Reason')}
            value={getTerminationReasonLabel(termination.reason, t)}
          />
        </>
      )}
      {termination?.request_sent != null && (
        <DetailRow
          label={t('Request fully sent')}
          value={termination.request_sent ? t('Yes') : t('No')}
        />
      )}
      {elapsedMs != null && elapsedMs > 0 && (
        <DetailRow
          label={t('Upstream elapsed')}
          value={formatUseTime(elapsedMs / 1000)}
          mono
        />
      )}
      {termination?.client_received_data != null && (
        <DetailRow
          label={t('Client received data')}
          value={termination.client_received_data ? t('Yes') : t('No')}
        />
      )}
      {props.transportError && (
        <DetailRow
          label={t('Transport Error')}
          value={props.transportError}
          mono
        />
      )}
    </DetailSection>
  )
}
