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
import { zodResolver } from '@hookform/resolvers/zod'
import { useMutation, useQueryClient } from '@tanstack/react-query'
import { useForm, type Resolver } from 'react-hook-form'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'
import { z } from 'zod'

import {
  Form,
  FormControl,
  FormDescription,
  FormField,
  FormItem,
  FormLabel,
  FormMessage,
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
import {
  SettingsForm,
  SettingsSwitchContent,
  SettingsSwitchItem,
} from '@/features/system-settings/components/settings-form-layout'
import { SettingsPageFormActions } from '@/features/system-settings/components/settings-page-context'

import { updateUsdtSettings } from '../../api'
import { USDT_QUERY_KEYS } from '../../lib'
import type { UsdtSettings, UsdtSettingsPatch } from '../../types'
import { UsdtChainFields } from './usdt-chain-fields'

const chainSchema = z.object({
  id: z.string(),
  enabled: z.boolean(),
  factory_address: z.string().trim(),
  rpc_url: z.string().trim(),
  rpc_url_secondary: z.string().trim(),
  finality: z.enum(['finalized', 'safe', 'confirmations']),
  confirmations: z.coerce.number().int().min(0).max(10000),
  min_amount: z.coerce.number().min(0),
  sweep_threshold: z.coerce.number().min(0),
})

const schema = z.object({
  enabled: z.boolean(),
  admin_only: z.boolean(),
  rate_mode: z.enum(['market', 'fixed']),
  fixed_rate: z.coerce.number().gt(0).max(100),
  scanner_ips: z.string().trim(),
  chains: z.array(chainSchema),
})

export type UsdtSettingsValues = z.infer<typeof schema>

function toFormValues(settings: UsdtSettings): UsdtSettingsValues {
  return {
    enabled: settings.enabled,
    admin_only: settings.admin_only,
    rate_mode: settings.rate_mode,
    fixed_rate: settings.fixed_rate,
    scanner_ips: settings.scanner_ips,
    chains: settings.chains.map((c) => ({
      id: c.id,
      enabled: c.enabled,
      factory_address: c.factory_display || c.factory_address,
      rpc_url: c.rpc_url,
      rpc_url_secondary: c.rpc_url_secondary,
      finality: c.finality,
      confirmations: c.confirmations,
      min_amount: c.min_amount,
      sweep_threshold: c.sweep_threshold,
    })),
  }
}

function toPatch(values: UsdtSettingsValues): UsdtSettingsPatch {
  const chains: UsdtSettingsPatch['chains'] = {}
  for (const c of values.chains) {
    chains[c.id] = {
      enabled: c.enabled,
      factory_address: c.factory_address,
      rpc_url: c.rpc_url,
      rpc_url_secondary: c.rpc_url_secondary,
      finality: c.finality,
      confirmations: c.confirmations,
      min_amount: c.min_amount,
      sweep_threshold: c.sweep_threshold,
    }
  }
  return {
    enabled: values.enabled,
    admin_only: values.admin_only,
    rate_mode: values.rate_mode,
    fixed_rate: values.fixed_rate,
    scanner_ips: values.scanner_ips,
    chains,
  }
}

export function UsdtSettingsForm(props: { settings: UsdtSettings }) {
  const { t } = useTranslation()
  const queryClient = useQueryClient()
  const form = useForm<UsdtSettingsValues>({
    resolver: zodResolver(schema) as unknown as Resolver<UsdtSettingsValues>,
    defaultValues: toFormValues(props.settings),
  })
  const save = useMutation({
    mutationFn: (values: UsdtSettingsValues) =>
      updateUsdtSettings(toPatch(values)),
    onSuccess: (settings) => {
      queryClient.setQueryData(USDT_QUERY_KEYS.settings, settings)
      form.reset(toFormValues(settings))
      toast.success(t('USDT settings saved'))
    },
  })
  const rateMode = form.watch('rate_mode')

  return (
    <Form {...form}>
      <SettingsForm
        onSubmit={form.handleSubmit((v) => save.mutate(v))}
        autoComplete='off'
      >
        <SettingsPageFormActions
          onSave={form.handleSubmit((v) => save.mutate(v))}
          isSaving={save.isPending}
          isSaveDisabled={!form.formState.isDirty}
          saveLabel='Save USDT settings'
        />
        <FormField
          control={form.control}
          name='enabled'
          render={({ field }) => (
            <SettingsSwitchItem>
              <SettingsSwitchContent>
                <FormLabel>{t('Enable USDT deposits')}</FormLabel>
                <FormDescription>
                  {t(
                    'Shows the USDT deposit card in the wallet for networks that are enabled and verified.'
                  )}
                </FormDescription>
              </SettingsSwitchContent>
              <FormControl>
                <Switch
                  checked={field.value}
                  onCheckedChange={field.onChange}
                />
              </FormControl>
            </SettingsSwitchItem>
          )}
        />
        <FormField
          control={form.control}
          name='admin_only'
          render={({ field }) => (
            <SettingsSwitchItem>
              <SettingsSwitchContent>
                <FormLabel>{t('Administrators only (testing)')}</FormLabel>
                <FormDescription>
                  {t(
                    'Only administrators see the USDT card and can get addresses or claim deposits. Turn it off to open USDT deposits to all users.'
                  )}
                </FormDescription>
              </SettingsSwitchContent>
              <FormControl>
                <Switch
                  checked={field.value}
                  onCheckedChange={field.onChange}
                />
              </FormControl>
            </SettingsSwitchItem>
          )}
        />
        <div className='grid gap-6 sm:grid-cols-3'>
          <FormField
            control={form.control}
            name='rate_mode'
            render={({ field }) => (
              <FormItem>
                <FormLabel>{t('Credit rate')}</FormLabel>
                <Select
                  items={[
                    { value: 'market', label: t('Market price (USDT/USD)') },
                    { value: 'fixed', label: t('Fixed rate') },
                  ]}
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
                      <SelectItem value='market'>
                        {t('Market price (USDT/USD)')}
                      </SelectItem>
                      <SelectItem value='fixed'>{t('Fixed rate')}</SelectItem>
                    </SelectGroup>
                  </SelectContent>
                </Select>
                <FormDescription>
                  {t(
                    'Market mode reads Coinbase, then Kraken, and holds deposits while no price is available.'
                  )}
                </FormDescription>
              </FormItem>
            )}
          />
          <FormField
            control={form.control}
            name='fixed_rate'
            render={({ field }) => (
              <FormItem>
                <FormLabel>{t('Fixed rate (USD per USDT)')}</FormLabel>
                <FormControl>
                  <Input
                    type='number'
                    step='0.0001'
                    min={0}
                    disabled={rateMode !== 'fixed'}
                    {...field}
                  />
                </FormControl>
                <FormMessage />
              </FormItem>
            )}
          />
          <FormField
            control={form.control}
            name='scanner_ips'
            render={({ field }) => (
              <FormItem>
                <FormLabel>{t('Scanner IP allowlist')}</FormLabel>
                <FormControl>
                  <Input placeholder='203.0.113.10' {...field} />
                </FormControl>
                <FormDescription>
                  {t(
                    'Comma-separated. Empty allows any IP with a valid signature.'
                  )}
                </FormDescription>
              </FormItem>
            )}
          />
        </div>
        {props.settings.chains.map((chain, index) => (
          <UsdtChainFields
            key={chain.id}
            chain={chain}
            index={index}
            control={form.control}
          />
        ))}
      </SettingsForm>
    </Form>
  )
}
