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
  termination: RelayTermination
  /** Masked transport error; only passed for admins. */
  transportError?: string
}

export function TerminationDetails(props: TerminationDetailsProps) {
  const { t } = useTranslation()
  const elapsedMs = props.termination.elapsed_ms
  return (
    <DetailSection
      icon={<Unplug className='size-3.5' aria-hidden='true' />}
      label={t('Connection Termination')}
      variant={props.termination.ended_by === 'client' ? 'default' : 'danger'}
    >
      <DetailRow
        label={t('Ended By')}
        value={
          <span
            className={cn(
              'font-medium',
              terminationTextClass(props.termination.ended_by)
            )}
          >
            {getTerminationEndedByLabel(props.termination.ended_by, t)}
          </span>
        }
      />
      <DetailRow
        label={t('Phase')}
        value={getTerminationPhaseLabel(props.termination.phase, t)}
      />
      <DetailRow
        label={t('Reason')}
        value={getTerminationReasonLabel(props.termination.reason, t)}
      />
      {props.termination.request_sent != null && (
        <DetailRow
          label={t('Request fully sent')}
          value={props.termination.request_sent ? t('Yes') : t('No')}
        />
      )}
      {elapsedMs != null && elapsedMs > 0 && (
        <DetailRow
          label={t('Upstream elapsed')}
          value={formatUseTime(elapsedMs / 1000)}
          mono
        />
      )}
      {props.termination.client_received_data != null && (
        <DetailRow
          label={t('Client received data')}
          value={props.termination.client_received_data ? t('Yes') : t('No')}
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
