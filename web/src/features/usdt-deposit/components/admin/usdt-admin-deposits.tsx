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
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { useState } from 'react'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'

import { ConfirmDialog } from '@/components/confirm-dialog'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { NativeSelect, NativeSelectOption } from '@/components/ui/native-select'
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from '@/components/ui/table'

import {
  creditUsdtDeposit,
  listUsdtDeposits,
  recheckUsdtTransaction,
} from '../../api'
import { isTxHash, summarizeOutcomes, USDT_QUERY_KEYS } from '../../lib'
import type { UsdtChainSetting, UsdtDeposit } from '../../types'
import { DepositRow } from '../usdt-deposit-history'
import { UsdtCollapsibleCard } from './usdt-collapsible-card'

const PAGE_SIZE = 20

export function UsdtAdminDeposits(props: { chains: UsdtChainSetting[] }) {
  const { t } = useTranslation()
  const queryClient = useQueryClient()
  const [page, setPage] = useState(1)
  const [status, setStatus] = useState('')
  const [userId, setUserId] = useState('')
  const [recheckChain, setRecheckChain] = useState(props.chains[0]?.id ?? 'bsc')
  const [recheckHash, setRecheckHash] = useState('')
  const [crediting, setCrediting] = useState<UsdtDeposit | null>(null)
  const filters = { status, user_id: userId.trim() }
  const query = useQuery({
    queryKey: USDT_QUERY_KEYS.adminDeposits(page, JSON.stringify(filters)),
    queryFn: () => listUsdtDeposits(page, PAGE_SIZE, filters),
  })
  const refresh = () =>
    queryClient.invalidateQueries({ queryKey: ['usdt', 'admin-deposits'] })
  const recheck = useMutation({
    mutationFn: () => recheckUsdtTransaction(recheckChain, recheckHash.trim()),
    onSuccess: (outcomes) => {
      const summary = summarizeOutcomes(outcomes)
      toast.success(
        t(
          'Checked {{total}} transfers: {{credited}} credited, {{already}} already credited, {{belowMin}} below minimum',
          {
            total: outcomes.length,
            credited: summary.credited,
            already: summary.already,
            belowMin: summary.belowMin,
          }
        )
      )
      setRecheckHash('')
      void refresh()
    },
  })
  const credit = useMutation({
    mutationFn: (id: number) => creditUsdtDeposit(id),
    onSuccess: () => {
      toast.success(t('Deposit credited'))
      setCrediting(null)
      void refresh()
    },
  })
  const items = query.data?.items ?? []
  const lastPage = Math.max(1, Math.ceil((query.data?.total ?? 0) / PAGE_SIZE))

  return (
    <UsdtCollapsibleCard
      title={t('USDT deposits')}
      summary={
        <span className='text-muted-foreground text-xs font-normal'>
          {t('{{count}} records', { count: query.data?.total ?? 0 })}
        </span>
      }
    >
      <form
        className='flex flex-col gap-2 sm:flex-row'
        onSubmit={(event) => {
          event.preventDefault()
          if (isTxHash(recheckHash)) recheck.mutate()
        }}
      >
        <NativeSelect
          aria-label={t('Network')}
          value={recheckChain}
          onChange={(event) => setRecheckChain(event.target.value)}
        >
          {props.chains.map((c) => (
            <NativeSelectOption key={c.id} value={c.id}>
              {c.name}
            </NativeSelectOption>
          ))}
        </NativeSelect>
        <Input
          value={recheckHash}
          onChange={(event) => setRecheckHash(event.target.value)}
          placeholder={t('Transaction hash to check and credit')}
          className='font-mono'
          aria-label={t('Transaction hash to check and credit')}
        />
        <Button
          type='submit'
          variant='outline'
          disabled={!isTxHash(recheckHash) || recheck.isPending}
        >
          {t('Check and credit')}
        </Button>
      </form>
      <div className='flex flex-col gap-2 sm:flex-row'>
        <NativeSelect
          aria-label={t('Status')}
          value={status}
          onChange={(event) => {
            setStatus(event.target.value)
            setPage(1)
          }}
        >
          <NativeSelectOption value=''>{t('All statuses')}</NativeSelectOption>
          <NativeSelectOption value='credited'>
            {t('Credited')}
          </NativeSelectOption>
          <NativeSelectOption value='below_min'>
            {t('Below minimum')}
          </NativeSelectOption>
        </NativeSelect>
        <Input
          value={userId}
          onChange={(event) => {
            setUserId(event.target.value)
            setPage(1)
          }}
          placeholder={t('User ID')}
          aria-label={t('User ID')}
          className='sm:max-w-40'
        />
      </div>
      {items.length === 0 ? (
        <p className='text-muted-foreground p-4 text-center text-sm'>
          {t('No USDT deposits yet')}
        </p>
      ) : (
        <Table>
          <TableHeader>
            <TableRow>
              <TableHead>{t('User ID')}</TableHead>
              <TableHead>{t('Network')}</TableHead>
              <TableHead>{t('Amount')}</TableHead>
              <TableHead>{t('Credited')}</TableHead>
              <TableHead>{t('Status')}</TableHead>
              <TableHead>{t('Transaction')}</TableHead>
              <TableHead>{t('Time')}</TableHead>
              <TableHead>{t('Actions')}</TableHead>
            </TableRow>
          </TableHeader>
          <TableBody>
            {items.map((item) => (
              <DepositRow
                key={item.id}
                item={item}
                showUser
                action={
                  <TableCell>
                    {item.status === 'below_min' && (
                      <Button
                        size='sm'
                        variant='outline'
                        onClick={() => setCrediting(item)}
                      >
                        {t('Credit manually')}
                      </Button>
                    )}
                  </TableCell>
                }
              />
            ))}
          </TableBody>
        </Table>
      )}
      {lastPage > 1 && (
        <div className='flex justify-end gap-2'>
          <Button
            size='sm'
            variant='outline'
            disabled={page <= 1}
            onClick={() => setPage((p) => p - 1)}
          >
            {t('Previous')}
          </Button>
          <Button
            size='sm'
            variant='outline'
            disabled={page >= lastPage}
            onClick={() => setPage((p) => p + 1)}
          >
            {t('Next')}
          </Button>
        </div>
      )}
      <ConfirmDialog
        open={crediting !== null}
        onOpenChange={(open) => {
          if (!open) setCrediting(null)
        }}
        title={t('Credit this deposit?')}
        desc={t(
          '{{amount}} USDT for user {{user}} is below the minimum. It is verified on chain again before crediting.',
          {
            amount: crediting?.amount ?? '',
            user: crediting?.user_id ?? '',
          }
        )}
        confirmText={t('Credit')}
        handleConfirm={() => {
          if (crediting) credit.mutate(crediting.id)
        }}
        isLoading={credit.isPending}
      />
    </UsdtCollapsibleCard>
  )
}
