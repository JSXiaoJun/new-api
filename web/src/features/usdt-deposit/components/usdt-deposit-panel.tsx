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
import { TriangleAlert } from 'lucide-react'
import { QRCodeSVG } from 'qrcode.react'
import { useState } from 'react'
import { useTranslation } from 'react-i18next'

import { CopyButton } from '@/components/copy-button'
import { Alert, AlertDescription } from '@/components/ui/alert'
import { Button } from '@/components/ui/button'
import { Skeleton } from '@/components/ui/skeleton'
import { ToggleGroup, ToggleGroupItem } from '@/components/ui/toggle-group'
import { formatCurrencyFromUSD } from '@/lib/currency'

import { createUsdtAddress, getUsdtInfo } from '../api'
import { USDT_QUERY_KEYS } from '../lib'
import type { UsdtNetwork } from '../types'
import { UsdtDepositHistory } from './usdt-deposit-history'

interface UsdtDepositPanelProps {
  /** Returns false (and prompts) while the purchase agreement is unsigned. */
  onBeforeAddress: () => boolean
  /** Called after a deposit was credited, to refresh the balance. */
  onCredited: () => void
}

/**
 * USDT deposit flow shown inside the wallet's "Add Funds" card when the user
 * picks USDT as the payment method.
 */
export function UsdtDepositPanel(props: UsdtDepositPanelProps) {
  const { t } = useTranslation()
  const queryClient = useQueryClient()
  const [selected, setSelected] = useState<string | null>(null)
  const infoQuery = useQuery({
    queryKey: USDT_QUERY_KEYS.info,
    queryFn: getUsdtInfo,
  })
  const addressMutation = useMutation({
    mutationFn: createUsdtAddress,
    onSuccess: () =>
      queryClient.invalidateQueries({ queryKey: USDT_QUERY_KEYS.info }),
  })

  const info = infoQuery.data
  const networks = info?.networks ?? []
  if (infoQuery.isLoading) {
    return <Skeleton className='h-64 w-full rounded-xl' />
  }
  if (!info?.enabled || networks.length === 0) {
    return (
      <p className='text-muted-foreground text-sm'>
        {t('USDT top-up is not available')}
      </p>
    )
  }
  const network: UsdtNetwork =
    networks.find((n) => n.id === selected) ?? networks[0]
  const address = info.addresses[network.id]

  return (
    <section className='space-y-5' aria-label={t('USDT Deposit')}>
      <p className='text-muted-foreground text-sm'>
        {t(
          'Each network gives you a permanent personal address. Deposits are credited automatically after network confirmation.'
        )}
      </p>
      <div className='space-y-2'>
        <p className='text-muted-foreground text-xs font-medium tracking-wider uppercase'>
          {t('Network')}
        </p>
        <ToggleGroup
          aria-label={t('Network')}
          value={[network.id]}
          onValueChange={(values) => {
            const next = networks.find((n) => values.includes(n.id))
            if (next) setSelected(next.id)
          }}
          spacing={2}
          className='grid w-full grid-cols-2 md:grid-cols-4'
        >
          {networks.map((n) => (
            <ToggleGroupItem
              key={n.id}
              value={n.id}
              variant='outline'
              className='data-[pressed]:border-primary data-[pressed]:bg-primary/5 h-auto min-h-11 px-2 py-2 text-sm whitespace-normal'
            >
              {n.name}
            </ToggleGroupItem>
          ))}
        </ToggleGroup>
      </div>

      <div className='flex flex-col gap-4 sm:flex-row sm:items-start'>
        {address ? (
          <>
            <div className='mx-auto shrink-0 rounded-lg border bg-white p-3 sm:mx-0'>
              <QRCodeSVG
                value={address}
                size={148}
                aria-label={t('Deposit address QR code')}
              />
            </div>
            <div className='min-w-0 flex-1 space-y-3'>
              <p className='text-muted-foreground text-xs'>
                {t('Your {{network}} deposit address', {
                  network: network.name,
                })}
              </p>
              <div className='bg-muted/50 flex items-center gap-2 rounded-lg border px-3 py-2'>
                <code className='min-w-0 flex-1 font-mono text-sm break-all'>
                  {address}
                </code>
                <CopyButton
                  value={address}
                  aria-label={t('Copy deposit address')}
                />
              </div>
              <DepositFacts network={network} rate={info.rate} />
            </div>
          </>
        ) : (
          <div className='flex-1 space-y-3'>
            <DepositFacts network={network} rate={info.rate} />
            <Button
              onClick={() => {
                if (props.onBeforeAddress()) addressMutation.mutate(network.id)
              }}
              disabled={addressMutation.isPending}
            >
              {t('Show my deposit address')}
            </Button>
          </div>
        )}
      </div>

      <Alert>
        <TriangleAlert aria-hidden='true' />
        <AlertDescription>
          {t(
            'Only send USDT on {{network}} to this address. Other tokens or networks are not credited automatically.',
            { network: network.name }
          )}
        </AlertDescription>
      </Alert>

      <UsdtDepositHistory onCredited={props.onCredited} />
    </section>
  )
}

function DepositFacts(props: { network: UsdtNetwork; rate?: string }) {
  const { t } = useTranslation()
  return (
    <dl className='grid grid-cols-2 gap-x-4 gap-y-1 text-sm'>
      <dt className='text-muted-foreground'>{t('Minimum deposit')}</dt>
      <dd>{props.network.min_amount} USDT</dd>
      <dt className='text-muted-foreground'>{t('Credit rate')}</dt>
      <dd>
        {props.rate
          ? t('1 USDT = {{amount}}', {
              amount: formatCurrencyFromUSD(Number(props.rate)),
            })
          : t('Unavailable')}
      </dd>
      <dt className='text-muted-foreground'>{t('Token contract')}</dt>
      <dd className='truncate font-mono text-xs' title={props.network.contract}>
        {props.network.contract}
      </dd>
    </dl>
  )
}
