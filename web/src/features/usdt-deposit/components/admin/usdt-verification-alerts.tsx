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
import { useMutation, useQueryClient } from '@tanstack/react-query'
import { ShieldAlert } from 'lucide-react'
import { useState } from 'react'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'

import { ConfirmDialog } from '@/components/confirm-dialog'
import { Alert, AlertDescription, AlertTitle } from '@/components/ui/alert'
import { Button } from '@/components/ui/button'
import { formatTimestamp } from '@/lib/format'

import { releaseUsdtConflict } from '../../api'
import { USDT_QUERY_KEYS } from '../../lib'
import type { UsdtVerificationAlert } from '../../types'

/**
 * Lists deposits held because two verification providers disagreed. One of
 * them is broken or compromised; nothing was credited, and a held deposit
 * stays held until an administrator releases it.
 */
export function UsdtVerificationAlerts(props: {
  alerts: UsdtVerificationAlert[]
}) {
  const { t } = useTranslation()
  const queryClient = useQueryClient()
  const [releasing, setReleasing] = useState<UsdtVerificationAlert | null>(null)
  const release = useMutation({
    mutationFn: (alert: UsdtVerificationAlert) =>
      releaseUsdtConflict(alert.chain, alert.tx_hash),
    onSuccess: () => {
      toast.success(t('Hold released. The deposit is verified again.'))
      setReleasing(null)
      void queryClient.invalidateQueries({ queryKey: USDT_QUERY_KEYS.settings })
    },
  })
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
            'These deposits were held because two providers returned different results. Check both providers before trusting either.'
          )}
        </p>
        <ul className='mt-2 space-y-2 text-xs'>
          {[...props.alerts].reverse().map((alert) => (
            <li
              key={`${alert.at}-${alert.tx_hash}`}
              className='space-y-1 break-all'
            >
              <div className='font-mono'>
                {formatTimestamp(alert.at)} {alert.chain.toUpperCase()}{' '}
                {alert.tx_hash}
              </div>
              <div>{alert.detail}</div>
              {alert.held && (
                <Button
                  type='button'
                  size='sm'
                  variant='outline'
                  onClick={() => setReleasing(alert)}
                >
                  {t('Release hold')}
                </Button>
              )}
            </li>
          ))}
        </ul>
      </AlertDescription>
      <ConfirmDialog
        open={releasing !== null}
        onOpenChange={(open) => {
          if (!open) setReleasing(null)
        }}
        title={t('Release this hold?')}
        desc={t(
          'Remove or fix the provider that reported wrong data first. The deposit is then verified again by two providers.'
        )}
        confirmText={t('Release hold')}
        handleConfirm={() => {
          if (releasing) release.mutate(releasing)
        }}
        isLoading={release.isPending}
      />
    </Alert>
  )
}
