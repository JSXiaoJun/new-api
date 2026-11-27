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
import { type Control, useFormState, useWatch } from 'react-hook-form'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'

import { StatusBadge } from '@/components/status-badge'
import { Button } from '@/components/ui/button'
import {
  FormControl,
  FormDescription,
  FormField,
  FormItem,
  FormLabel,
} from '@/components/ui/form'
import { Input } from '@/components/ui/input'
import {
  Select,
  SelectContent,
  SelectGroup,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from '@/components/ui/select'
import { Switch } from '@/components/ui/switch'

import { verifyUsdtFactory } from '../../api'
import { NETWORK_STATUS, USDT_QUERY_KEYS } from '../../lib'
import type { UsdtChainSetting } from '../../types'
import { UsdtCollapsibleCard } from './usdt-collapsible-card'
import { UsdtRpcNodeList } from './usdt-rpc-node-list'
import type { UsdtSettingsValues } from './usdt-settings-form'

interface UsdtChainFieldsProps {
  chain: UsdtChainSetting
  index: number
  control: Control<UsdtSettingsValues>
}

export function UsdtChainFields(props: UsdtChainFieldsProps) {
  const { t } = useTranslation()
  const queryClient = useQueryClient()
  const prefix = `chains.${props.index}` as const
  const finality = useWatch({
    control: props.control,
    name: `${prefix}.finality`,
  })
  const { isDirty } = useFormState({ control: props.control })
  const verify = useMutation({
    mutationFn: () => verifyUsdtFactory(props.chain.id),
    onSuccess: (result) => {
      toast.success(
        t('Factory verified. Funds go to {{destination}}', {
          destination: result.destination,
        })
      )
      void queryClient.invalidateQueries({ queryKey: USDT_QUERY_KEYS.settings })
    },
  })
  const isTron = props.chain.id === 'tron'
  const finalityOptions = [
    { value: 'finalized', label: t('Finalized block') },
    ...(isTron ? [] : [{ value: 'safe', label: t('Safe block') }]),
    { value: 'confirmations', label: t('Confirmation count') },
  ]

  const networkStatus = NETWORK_STATUS[props.chain.status ?? 'normal']

  return (
    <UsdtCollapsibleCard
      title={props.chain.name}
      summary={
        <>
          <StatusBadge
            label={props.chain.verified ? t('Verified') : t('Not verified')}
            variant={props.chain.verified ? 'success' : 'warning'}
          />
          <StatusBadge
            label={props.chain.enabled ? t('Shown to users') : t('Hidden')}
            variant={props.chain.enabled ? 'info' : 'neutral'}
          />
          <StatusBadge
            label={t(networkStatus.labelKey)}
            variant={networkStatus.variant}
          />
          <span className='text-muted-foreground text-xs font-normal'>
            {t('{{count}} addresses issued', {
              count: props.chain.address_count,
            })}
            {' · '}
            {t('{{available}}/{{providers}} providers available', {
              available: props.chain.available_providers,
              providers: props.chain.providers,
            })}
          </span>
        </>
      }
    >
      <p className='text-muted-foreground font-mono text-xs break-all'>
        {t('USDT contract')}: {props.chain.contract}
      </p>
      <FormField
        control={props.control}
        name={`${prefix}.enabled`}
        render={({ field }) => (
          <FormItem className='flex items-center justify-between gap-4'>
            <div>
              <FormLabel>{t('Show this network to users')}</FormLabel>
              <FormDescription>
                {t(
                  'Requires a verified factory. Hidden networks are still scanned and credited.'
                )}
              </FormDescription>
            </div>
            <FormControl>
              <Switch
                checked={field.value}
                onCheckedChange={field.onChange}
                disabled={!props.chain.verified}
              />
            </FormControl>
          </FormItem>
        )}
      />
      <div className='grid gap-4 md:grid-cols-2'>
        <FormField
          control={props.control}
          name={`${prefix}.factory_address`}
          render={({ field }) => (
            <FormItem>
              <FormLabel>{t('Factory contract address')}</FormLabel>
              <FormControl>
                <Input
                  className='font-mono'
                  disabled={props.chain.factory_locked}
                  {...field}
                />
              </FormControl>
              <FormDescription>
                {props.chain.factory_locked
                  ? t(
                      'Locked: addresses were already issued from this factory.'
                    )
                  : t(
                      'Printed by usdt-deploy. Changing it clears verification.'
                    )}
              </FormDescription>
            </FormItem>
          )}
        />
        <FormField
          control={props.control}
          name={`${prefix}.finality`}
          render={({ field }) => (
            <FormItem>
              <FormLabel>{t('Finality')}</FormLabel>
              <Select
                items={finalityOptions}
                value={field.value}
                onValueChange={field.onChange}
              >
                <FormControl>
                  <SelectTrigger>
                    <SelectValue />
                  </SelectTrigger>
                </FormControl>
                <SelectContent alignItemWithTrigger={false}>
                  <SelectGroup>
                    {finalityOptions.map((o) => (
                      <SelectItem key={o.value} value={o.value}>
                        {o.label}
                      </SelectItem>
                    ))}
                  </SelectGroup>
                </SelectContent>
              </Select>
            </FormItem>
          )}
        />
        <FormField
          control={props.control}
          name={`${prefix}.confirmations`}
          render={({ field }) => (
            <FormItem>
              <FormLabel>{t('Confirmations')}</FormLabel>
              <FormControl>
                <Input
                  type='number'
                  min={1}
                  disabled={finality !== 'confirmations'}
                  {...field}
                />
              </FormControl>
            </FormItem>
          )}
        />
        <FormField
          control={props.control}
          name={`${prefix}.min_amount`}
          render={({ field }) => (
            <FormItem>
              <FormLabel>{t('Minimum deposit (USDT)')}</FormLabel>
              <FormControl>
                <Input type='number' min={0} step='0.01' {...field} />
              </FormControl>
              <FormDescription>
                {t('Smaller deposits are recorded but need manual crediting.')}
              </FormDescription>
            </FormItem>
          )}
        />
        <FormField
          control={props.control}
          name={`${prefix}.sweep_threshold`}
          render={({ field }) => (
            <FormItem>
              <FormLabel>{t('Sweep threshold (USDT)')}</FormLabel>
              <FormControl>
                <Input type='number' min={0} step='0.01' {...field} />
              </FormControl>
              <FormDescription>
                {t(
                  'The scanner forwards a wallet once its balance reaches this amount.'
                )}
              </FormDescription>
            </FormItem>
          )}
        />
        <UsdtRpcNodeList
          chain={props.chain}
          chainIndex={props.index}
          control={props.control}
        />
      </div>
      <div className='flex flex-wrap items-center gap-3'>
        <Button
          type='button'
          variant='outline'
          size='sm'
          disabled={
            verify.isPending ||
            isDirty ||
            !props.chain.factory_address ||
            props.chain.providers < 2
          }
          onClick={() => verify.mutate()}
        >
          {t('Verify factory')}
        </Button>
        <span className='text-muted-foreground text-xs'>
          {isDirty
            ? t('Save your changes before verifying.')
            : t(
                'Checks that new-api derives exactly the addresses the factory deploys.'
              )}
        </span>
      </div>
    </UsdtCollapsibleCard>
  )
}
