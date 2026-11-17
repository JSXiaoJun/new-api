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

import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import { formatQuota } from '@/lib/format'

import { claimUsdtDeposit } from '../api'
import { isTxHash, summarizeOutcomes } from '../lib'
import type { UsdtNetwork } from '../types'

interface UsdtClaimFormProps {
  network: UsdtNetwork
  onCredited: () => void
}

/** Lets a user submit a transaction hash instead of waiting for the scanner. */
export function UsdtClaimForm(props: UsdtClaimFormProps) {
  const { t } = useTranslation()
  const queryClient = useQueryClient()
  const [txHash, setTxHash] = useState('')
  const valid = isTxHash(txHash)
  const claim = useMutation({
    mutationFn: () => claimUsdtDeposit(props.network.id, txHash.trim()),
    onSuccess: (outcomes) => {
      const summary = summarizeOutcomes(outcomes)
      if (summary.credited > 0) {
        toast.success(
          t('Deposit credited: {{quota}}', {
            quota: formatQuota(summary.creditedQuota),
          })
        )
        props.onCredited()
      } else if (summary.belowMin > 0) {
        toast.warning(
          t(
            'The deposit is below the minimum amount and was not credited automatically. Contact support.'
          )
        )
      } else {
        toast.info(t('This deposit was already credited'))
      }
      setTxHash('')
      void queryClient.invalidateQueries({ queryKey: ['usdt', 'deposits'] })
    },
  })

  return (
    <form
      className='space-y-2'
      onSubmit={(event) => {
        event.preventDefault()
        if (valid) claim.mutate()
      }}
    >
      <Label htmlFor='usdt-claim-tx'>
        {t('Already paid? Check by transaction hash')}
      </Label>
      <div className='flex flex-col gap-2 sm:flex-row'>
        <Input
          id='usdt-claim-tx'
          value={txHash}
          onChange={(event) => setTxHash(event.target.value)}
          placeholder={t('Transaction hash (TxID)')}
          className='font-mono'
          aria-invalid={txHash !== '' && !valid}
          autoComplete='off'
        />
        <Button
          type='submit'
          variant='outline'
          disabled={!valid || claim.isPending}
        >
          {t('Check deposit')}
        </Button>
      </div>
    </form>
  )
}
