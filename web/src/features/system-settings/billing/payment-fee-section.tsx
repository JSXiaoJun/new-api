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
import { Switch } from '@/components/ui/switch'

import {
  SettingsForm,
  SettingsSwitchContent,
  SettingsSwitchItem,
} from '../components/settings-form-layout'
import { SettingsPageFormActions } from '../components/settings-page-context'
import { SettingsSection } from '../components/settings-section'
import { useUpdateOption } from '../hooks/use-update-option'

// Must match operation_setting.MaxEpayFeeRate on the server.
export const MAX_EPAY_FEE_RATE = 20

const schema = z.object({
  enabled: z.boolean(),
  threshold: z.coerce.number().min(0),
  rate: z.coerce.number().min(0).max(MAX_EPAY_FEE_RATE),
})

export type PaymentFeeValues = z.infer<typeof schema>

// Threshold and rate are saved before the switch so turning the fee on never
// applies values the administrator has not saved yet.
const FIELDS: Array<{ name: keyof PaymentFeeValues; key: string }> = [
  { name: 'threshold', key: 'payment_setting.epay_fee_threshold' },
  { name: 'rate', key: 'payment_setting.epay_fee_rate' },
  { name: 'enabled', key: 'payment_setting.epay_fee_enabled' },
]

export function PaymentFeeSection(props: { defaultValues: PaymentFeeValues }) {
  const { t } = useTranslation()
  const updateOption = useUpdateOption()
  const form = useForm<PaymentFeeValues>({
    resolver: zodResolver(schema) as unknown as Resolver<PaymentFeeValues>,
    defaultValues: props.defaultValues,
  })
  const { isDirty, isSubmitting, dirtyFields } = form.formState
  const isSaving = updateOption.isPending || isSubmitting

  async function onSubmit(values: PaymentFeeValues) {
    const changed = FIELDS.filter((item) => dirtyFields[item.name])
    if (changed.length === 0) {
      toast.info(t('No changes to save'))
      return
    }
    for (const item of changed) {
      try {
        await updateOption.mutateAsync({
          key: item.key,
          value: String(values[item.name]),
        })
      } catch {
        return
      }
      form.resetField(item.name, { defaultValue: values[item.name] })
    }
  }

  return (
    <SettingsSection title={t('Payment Fee')}>
      <Form {...form}>
        <SettingsForm onSubmit={form.handleSubmit(onSubmit)} autoComplete='off'>
          <SettingsPageFormActions
            onSave={form.handleSubmit(onSubmit)}
            isSaving={isSaving}
            isSaveDisabled={!isDirty}
            saveLabel='Save payment fee settings'
          />
          <p className='text-muted-foreground text-sm'>
            {t(
              'Applies to Epay top-ups. When the amount to pay is below the threshold, the fee is added to the order and paid by the user. At or above the threshold, the merchant bears the gateway fee and the user pays the original amount. The credited quota is not changed.'
            )}
          </p>
          <FormField
            control={form.control}
            name='enabled'
            render={({ field }) => (
              <SettingsSwitchItem>
                <SettingsSwitchContent>
                  <FormLabel>
                    {t('Charge the fee to users below the threshold')}
                  </FormLabel>
                  <FormDescription>
                    {t(
                      'When off, the merchant bears the gateway fee on every order.'
                    )}
                  </FormDescription>
                </SettingsSwitchContent>
                <FormControl>
                  <Switch
                    checked={field.value}
                    onCheckedChange={field.onChange}
                    disabled={isSaving}
                  />
                </FormControl>
              </SettingsSwitchItem>
            )}
          />
          <div className='grid gap-6 sm:grid-cols-2'>
            <FormField
              control={form.control}
              name='threshold'
              render={({ field }) => (
                <FormItem>
                  <FormLabel>{t('Fee threshold (CNY)')}</FormLabel>
                  <FormControl>
                    <Input type='number' min={0} step='0.01' {...field} />
                  </FormControl>
                  <FormDescription>
                    {t(
                      'Orders whose amount to pay is below this value add the fee. Orders equal to or above it do not.'
                    )}
                  </FormDescription>
                  <FormMessage />
                </FormItem>
              )}
            />
            <FormField
              control={form.control}
              name='rate'
              render={({ field }) => (
                <FormItem>
                  <FormLabel>{t('Fee rate (%)')}</FormLabel>
                  <FormControl>
                    <Input
                      type='number'
                      min={0}
                      max={MAX_EPAY_FEE_RATE}
                      step='0.01'
                      {...field}
                    />
                  </FormControl>
                  <FormDescription>
                    {t(
                      'For example, 0.6 adds 0.6% of the amount, rounded up to the cent. At most {{max}}%.',
                      { max: MAX_EPAY_FEE_RATE }
                    )}
                  </FormDescription>
                  <FormMessage />
                </FormItem>
              )}
            />
          </div>
        </SettingsForm>
      </Form>
    </SettingsSection>
  )
}
