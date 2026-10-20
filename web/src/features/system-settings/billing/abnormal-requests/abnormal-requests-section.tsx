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
import { useState } from 'react'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'

import { ConfirmDialog } from '@/components/confirm-dialog'
import { NativeSelect, NativeSelectOption } from '@/components/ui/native-select'
import { Tabs, TabsContent, TabsList, TabsTrigger } from '@/components/ui/tabs'
import { formatQuotaWithCurrency } from '@/lib/currency'
import { handleServerError } from '@/lib/handle-server-error'

import { SettingsSection } from '../../components/settings-section'
import {
  AbnormalRecordsPanel,
  type RecordSelection,
  type RecordsUserFilter,
} from './abnormal-records-panel'
import {
  AbnormalRulesForm,
  type AbnormalRulesValues,
} from './abnormal-rules-form'
import { AbnormalUsersPanel } from './abnormal-users-panel'
import { recoverUserAbnormalCharges } from './api'
import type { AbnormalChargeRecord } from './types'

const PERIODS = [1, 7, 30, 90]

type PendingRecovery = {
  user: { user_id: number; username: string; max_pending_id?: number }
  records?: AbnormalChargeRecord[]
  count: number
  quota: number
}

export function AbnormalRequestsSection(props: {
  defaultValues: AbnormalRulesValues
}) {
  const { t } = useTranslation()
  const queryClient = useQueryClient()
  const [days, setDays] = useState(1)
  const [tab, setTab] = useState('users')
  const [userFilter, setUserFilter] = useState<RecordsUserFilter>({
    keyword: '',
  })
  const [selected, setSelected] = useState<RecordSelection>(() => new Map())
  const [pendingRecovery, setPendingRecovery] =
    useState<PendingRecovery | null>(null)

  const refresh = () =>
    Promise.all([
      queryClient.invalidateQueries({ queryKey: ['abnormal-charge-users'] }),
      queryClient.invalidateQueries({ queryKey: ['abnormal-charge-records'] }),
    ])

  const recover = useMutation({
    mutationFn: (request: PendingRecovery) =>
      recoverUserAbnormalCharges(request.user.user_id, {
        ids: request.records?.map((record) => record.id),
        days,
        // Records that arrive after the admin reviewed the totals are not
        // charged in the same recovery.
        max_id: request.records ? undefined : request.user.max_pending_id,
      }),
    onSuccess: async (result) => {
      toast.success(
        t('Recovered {{amount}} from {{count}} records', {
          amount: formatQuotaWithCurrency(result.quota),
          count: result.recovered_records,
        })
      )
      setPendingRecovery(null)
      setSelected(new Map())
      await refresh()
    },
    onError: (error) =>
      handleServerError(error, t('Failed to recover charges')),
  })

  return (
    <SettingsSection title={t('Abnormal Requests')}>
      <AbnormalRulesForm defaultValues={props.defaultValues} />

      <Tabs value={tab} onValueChange={(value) => setTab(String(value))}>
        <div className='flex flex-wrap items-center justify-between gap-2'>
          <TabsList aria-label={t('Abnormal request views')}>
            <TabsTrigger value='users'>{t('By user')}</TabsTrigger>
            <TabsTrigger value='records'>{t('All records')}</TabsTrigger>
          </TabsList>
          <NativeSelect
            aria-label={t('Review period')}
            value={days}
            onChange={(event) => {
              setDays(Number(event.target.value))
              setSelected(new Map())
            }}
          >
            {PERIODS.map((period) => (
              <NativeSelectOption key={period} value={period}>
                {t('Last {{days}} days', { days: period })}
              </NativeSelectOption>
            ))}
          </NativeSelect>
        </div>
        <TabsContent value='users'>
          <AbnormalUsersPanel
            key={days}
            days={days}
            onShowRecords={(user) => {
              setUserFilter({ userId: user.user_id, username: user.username })
              setSelected(new Map())
              setTab('records')
            }}
            onRecoverAll={(user) =>
              setPendingRecovery({
                user,
                count: user.pending_count,
                quota: user.pending_quota,
              })
            }
            onChanged={refresh}
          />
        </TabsContent>
        <TabsContent value='records'>
          <AbnormalRecordsPanel
            key={days}
            days={days}
            userFilter={userFilter}
            onUserFilterChange={(filter) => {
              setUserFilter(filter)
              setSelected(new Map())
            }}
            selected={selected}
            onSelectedChange={setSelected}
            onRecover={(user, records) =>
              setPendingRecovery({
                user,
                records,
                count: records.length,
                quota: records.reduce(
                  (sum, record) => sum + record.forgone_quota,
                  0
                ),
              })
            }
          />
        </TabsContent>
      </Tabs>

      <ConfirmDialog
        open={pendingRecovery !== null}
        onOpenChange={(open) =>
          !open && !recover.isPending && setPendingRecovery(null)
        }
        title={t('Recover uncollected charges')}
        desc={
          pendingRecovery
            ? t(
                'Charge {{username}} {{amount}} for {{count}} abnormal requests. The balance may become negative. Each record can be recovered only once.',
                {
                  username:
                    pendingRecovery.user.username ||
                    pendingRecovery.user.user_id,
                  amount: formatQuotaWithCurrency(pendingRecovery.quota),
                  count: pendingRecovery.count,
                }
              )
            : ''
        }
        confirmText={t('Recover')}
        destructive
        isLoading={recover.isPending}
        handleConfirm={() => pendingRecovery && recover.mutate(pendingRecovery)}
      />
    </SettingsSection>
  )
}
