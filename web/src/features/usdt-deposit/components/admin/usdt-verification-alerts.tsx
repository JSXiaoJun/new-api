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
import { ShieldAlert } from 'lucide-react'
import { useTranslation } from 'react-i18next'

import { Alert, AlertDescription, AlertTitle } from '@/components/ui/alert'
import { formatTimestamp } from '@/lib/format'

import type { UsdtVerificationAlert } from '../../types'

/**
 * Lists deposits held because the two verification nodes disagreed. One of
 * the providers is broken or compromised; nothing was credited.
 */
export function UsdtVerificationAlerts(props: {
  alerts: UsdtVerificationAlert[]
}) {
  const { t } = useTranslation()
  if (props.alerts.length === 0) {
    return null
  }
  return (
    <Alert variant='destructive'>
      <ShieldAlert aria-hidden='true' />
      <AlertTitle>{t('Verification nodes disagree')}</AlertTitle>
      <AlertDescription>
        <p>
          {t(
            'These deposits were held because the two providers returned different results. Check both providers before trusting either.'
          )}
        </p>
        <ul className='mt-2 space-y-1 font-mono text-xs'>
          {[...props.alerts].reverse().map((alert) => (
            <li key={`${alert.at}-${alert.tx_hash}`} className='break-all'>
              {formatTimestamp(alert.at)} {alert.chain.toUpperCase()}{' '}
              {alert.tx_hash}
            </li>
          ))}
        </ul>
      </AlertDescription>
    </Alert>
  )
}
