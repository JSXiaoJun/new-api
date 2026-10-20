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
  align_with_official: boolean
  charge_client_disconnect_before_response: boolean
  charge_client_disconnect_during_response: boolean
  charge_upstream_interrupt_during_response: boolean
  continue_after_cloudflare_truncation: boolean
  match_upstream_charge: boolean
  charge_upstream_billed_failure: boolean
}

const ALIGN_SWITCH_NAME = 'align_with_official'

const SWITCHES: Array<{
  name: keyof AbnormalBillingValues
  labelKey: string
  descriptionKey: string
  ignoredWhenAligned?: boolean
}> = [
  {
    name: ALIGN_SWITCH_NAME,
    labelKey: 'Align abnormal billing with official new-api',
    descriptionKey:
      'When on, abnormal requests are billed exactly like official new-api and the three switches below are ignored: a request that fails before upstream responds is refunded in full, an interrupted response is charged the usage produced so far, a relay stops as soon as the client disconnects, and a task submit whose caller disconnects is cancelled and refunded even if upstream already accepted it. The Cloudflare truncation switch still applies.',
  },
  {
    ignoredWhenAligned: true,
    name: 'charge_client_disconnect_before_response',
    labelKey: 'Charge when the client disconnects before upstream responds',
    descriptionKey:
      'The request already reached upstream but the client left before the first byte arrived. When on, the estimated input is charged; when off, the reservation is refunded in full.',
  },
  {
    ignoredWhenAligned: true,
    name: 'charge_client_disconnect_during_response',
    labelKey: 'Charge when the client disconnects while receiving the response',
    descriptionKey:
      'Upstream had started answering when the client left. When on, the usage produced so far is charged; when off, nothing is charged. Turning this off lets a client read output and disconnect before completion without paying.',
  },
  {
    ignoredWhenAligned: true,
    name: 'charge_upstream_interrupt_during_response',
    labelKey:
      'Charge when upstream or the gateway interrupts the response midway',
    descriptionKey:
      'The upstream connection broke, the stream timed out, or the stream ended without its completion event. When on, the usage produced so far is charged; when off, nothing is charged.',
  },
  {
    name: 'continue_after_cloudflare_truncation',
    labelKey:
      'Keep reading upstream after a Cloudflare truncation and charge actual usage',
    descriptionKey:
      'Applies to requests through Cloudflare that it cut after a long idle period (90 seconds by default). When on, the gateway keeps reading upstream to the end and charges the actual usage; the client still receives nothing more. When off, the relay stops right away like official new-api. This switch works whether or not the official alignment is on.',
  },
  {
    name: 'match_upstream_charge',
    labelKey: 'Charge whatever upstream billed for abandoned requests',
    descriptionKey:
      'When on, a user always pays what upstream billed for a request that ended early, and the switches above no longer waive it; only a request that failed before upstream answered stays free. On channels that keep reading upstream (Sub2API by default), the usage upstream reported is charged afterwards, so the balance may go negative. Non-stream requests on Sub2API and New API channels are then also read to the end, because those upstreams bill them in full. Works whether or not the official alignment is on, and overrides its refund of a client that left.',
  },
  {
    name: 'charge_upstream_billed_failure',
    labelKey: 'Charge what upstream billed for a request it broke off',
    descriptionKey:
      'Upstream such as Sub2API bills the usage it already reported when the response breaks midway (a Claude stream reports the input first), while the user only gets an error. When on, such a failed request is charged exactly that usage instead of being refunded; earlier failed retries are not charged, only the last attempt. When off, it is refunded in full as before. Per-call priced models are always refunded.',
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
  const alignedWithOfficial = form.watch(ALIGN_SWITCH_NAME)

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
                      disabled={
                        isSaving ||
                        (item.ignoredWhenAligned === true &&
                          alignedWithOfficial)
                      }
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
