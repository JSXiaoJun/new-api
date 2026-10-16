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
import { useForm } from 'react-hook-form'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'

import {
  Form,
  FormControl,
  FormDescription,
  FormField,
  FormLabel,
} from '@/components/ui/form'
import { Switch } from '@/components/ui/switch'

import {
  SettingsForm,
  SettingsSwitchContent,
  SettingsSwitchItem,
} from '../components/settings-form-layout'
import { SettingsPageFormActions } from '../components/settings-page-context'
import { SettingsSection } from '../components/settings-section'
import { useUpdateOption } from '../hooks/use-update-option'

export type AbnormalBillingValues = {
  charge_client_disconnect_before_response: boolean
  charge_client_disconnect_during_response: boolean
  charge_upstream_interrupt_during_response: boolean
}

const SWITCHES: Array<{
  name: keyof AbnormalBillingValues
  labelKey: string
  descriptionKey: string
}> = [
  {
    name: 'charge_client_disconnect_before_response',
    labelKey: 'Charge when the client disconnects before upstream responds',
    descriptionKey:
      'The request already reached upstream but the client left before the first byte arrived. When on, the estimated input is charged; when off, the reservation is refunded in full.',
  },
  {
    name: 'charge_client_disconnect_during_response',
    labelKey: 'Charge when the client disconnects while receiving the response',
    descriptionKey:
      'Upstream had started answering when the client left. When on, the usage produced so far is charged; when off, nothing is charged. Turning this off lets a client read output and disconnect before completion without paying.',
  },
  {
    name: 'charge_upstream_interrupt_during_response',
    labelKey:
      'Charge when upstream or the gateway interrupts the response midway',
    descriptionKey:
      'The upstream connection broke, the stream timed out, or the stream ended without its completion event. When on, the usage produced so far is charged; when off, nothing is charged.',
  },
]

const OPTION_PREFIX = 'abnormal_billing_setting.'

export function AbnormalBillingSection(props: {
  defaultValues: AbnormalBillingValues
}) {
  const { t } = useTranslation()
  const updateOption = useUpdateOption()
  const form = useForm<AbnormalBillingValues>({
    defaultValues: props.defaultValues,
  })
  const { isDirty, isSubmitting, dirtyFields } = form.formState
  const isSaving = updateOption.isPending || isSubmitting

  async function onSubmit(values: AbnormalBillingValues) {
    const changed = SWITCHES.filter((item) => dirtyFields[item.name])
    if (changed.length === 0) {
      toast.info(t('No changes to save'))
      return
    }
    // Each switch is saved on its own, so a rejected save keeps only that
    // switch dirty while the accepted ones become the new baseline.
    for (const item of changed) {
      try {
        await updateOption.mutateAsync({
          key: OPTION_PREFIX + item.name,
          value: String(values[item.name]),
        })
      } catch {
        return
      }
      form.resetField(item.name, { defaultValue: values[item.name] })
    }
  }

  return (
    <SettingsSection title={t('Abnormal Billing Control')}>
      <Form {...form}>
        <SettingsForm onSubmit={form.handleSubmit(onSubmit)} autoComplete='off'>
          <SettingsPageFormActions
            onSave={form.handleSubmit(onSubmit)}
            isSaving={isSaving}
            isSaveDisabled={!isDirty}
            saveLabel='Save abnormal billing settings'
          />
          <p className='text-muted-foreground text-sm'>
            {t(
              'Decide whether requests that ended abnormally are still charged. Upstream or gateway failures before any response are always refunded in full. A response that already delivered its completion event is always charged.'
            )}
          </p>
          {SWITCHES.map((item) => (
            <FormField
              key={item.name}
              control={form.control}
              name={item.name}
              render={({ field }) => (
                <SettingsSwitchItem>
                  <SettingsSwitchContent>
                    <FormLabel>{t(item.labelKey)}</FormLabel>
                    <FormDescription>{t(item.descriptionKey)}</FormDescription>
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
          ))}
        </SettingsForm>
      </Form>
    </SettingsSection>
  )
}
