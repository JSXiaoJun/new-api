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
import { useTranslation } from 'react-i18next'

import { ErrorState } from '@/components/error-state'
import { Skeleton } from '@/components/ui/skeleton'
import { SettingsSection } from '@/features/system-settings/components/settings-section'

import { getUsdtSettings } from '../../api'
import { USDT_QUERY_KEYS } from '../../lib'
import { UsdtAdminDeposits } from './usdt-admin-deposits'
import { UsdtScannerPanel } from './usdt-scanner-panel'
import { UsdtSettingsForm } from './usdt-settings-form'
import { UsdtVerificationAlerts } from './usdt-verification-alerts'

export function UsdtSettingsSection() {
  const { t } = useTranslation()
  const query = useQuery({
    queryKey: USDT_QUERY_KEYS.settings,
    queryFn: getUsdtSettings,
    refetchInterval: 60_000,
  })

  return (
    <SettingsSection title={t('USDT Deposits')}>
      <p className='text-muted-foreground text-sm'>
        {t(
          'Users get a permanent deposit address per network from your DepositFactory contract. The scanner service reports deposits and forwards them to your destination wallet; new-api verifies every deposit on chain before crediting it.'
        )}
      </p>
      {query.isLoading && <Skeleton className='h-96 w-full' />}
      {query.isError && <ErrorState onRetry={() => query.refetch()} />}
      {query.data && (
        <>
          <UsdtSettingsForm
            key={query.data.chains
              .map((c) => `${c.id}:${c.verified}:${c.factory_address}`)
              .join('|')}
            settings={query.data}
          />
          <UsdtVerificationAlerts alerts={query.data.alerts ?? []} />
          <UsdtScannerPanel settings={query.data} />
          <UsdtAdminDeposits chains={query.data.chains} />
        </>
      )}
    </SettingsSection>
  )
}
