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
import { useTranslation } from 'react-i18next'
import * as z from 'zod'

import { Checkbox } from '@/components/ui/checkbox'
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
import { Textarea } from '@/components/ui/textarea'

import {
  SettingsForm,
  SettingsSwitchContent,
  SettingsSwitchItem,
} from '../components/settings-form-layout'
import { SettingsPageFormActions } from '../components/settings-page-context'
import { SettingsSection } from '../components/settings-section'
import { useSettingsForm } from '../hooks/use-settings-form'
import { useUpdateOption } from '../hooks/use-update-option'
import { trafficControlWarningDefaults } from './traffic-control-defaults'

const trafficControlSchema = z.object({
  traffic_control: z.object({
    mainland_web_block_enabled: z.boolean(),
    include_hong_kong_taiwan: z.boolean(),
    country_header: z.string().trim().min(1),
    warning_title: z.string(),
    warning_content: z.string(),
    warning_annotation: z.string(),
  }),
})

type TrafficControlFormValues = z.infer<typeof trafficControlSchema>

type TrafficControlSectionProps = {
  defaultValues: {
    'traffic_control.mainland_web_block_enabled': boolean
    'traffic_control.include_hong_kong_taiwan': boolean
    'traffic_control.country_header': string
    'traffic_control.warning_title': string
    'traffic_control.warning_content': string
    'traffic_control.warning_annotation': string
  }
}

export function TrafficControlSection(props: TrafficControlSectionProps) {
  const { t } = useTranslation()
  const updateOption = useUpdateOption()
  const { form, handleSubmit, isSubmitting } =
    useSettingsForm<TrafficControlFormValues>({
      resolver: zodResolver(trafficControlSchema),
      defaultValues: {
        traffic_control: {
          mainland_web_block_enabled:
            props.defaultValues['traffic_control.mainland_web_block_enabled'],
          include_hong_kong_taiwan:
            props.defaultValues['traffic_control.include_hong_kong_taiwan'],
          country_header: props.defaultValues['traffic_control.country_header'],
          warning_title:
            props.defaultValues['traffic_control.warning_title'] ??
            trafficControlWarningDefaults['traffic_control.warning_title'],
          warning_content:
            props.defaultValues['traffic_control.warning_content'] ??
            trafficControlWarningDefaults['traffic_control.warning_content'],
          warning_annotation:
            props.defaultValues['traffic_control.warning_annotation'] ??
            trafficControlWarningDefaults['traffic_control.warning_annotation'],
        },
      },
      onSubmit: async (_data, changedFields) => {
        for (const [key, value] of Object.entries(changedFields)) {
          await updateOption.mutateAsync({
            key,
            value: value as string | boolean,
          })
        }
      },
    })

  return (
    <SettingsSection title={t('Traffic Control')}>
      <Form {...form}>
        <SettingsForm onSubmit={handleSubmit}>
          <SettingsPageFormActions
            onSave={handleSubmit}
            isSaving={isSubmitting || updateOption.isPending}
          />
          <FormField
            control={form.control}
            name='traffic_control.mainland_web_block_enabled'
            render={({ field }) => (
              <SettingsSwitchItem>
                <SettingsSwitchContent>
                  <FormLabel>{t('Block mainland China web traffic')}</FormLabel>
                  <FormDescription>
                    {t(
                      'Redirect visitors from selected regions to the access warning page while keeping API routes available.'
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
            name='traffic_control.include_hong_kong_taiwan'
            render={({ field }) => (
              <SettingsSwitchItem>
                <SettingsSwitchContent>
                  <FormLabel>{t('Also block Hong Kong and Taiwan')}</FormLabel>
                  <FormDescription>
                    {t(
                      'When enabled, visitors identified as HK or TW will see the same warning page.'
                    )}
                  </FormDescription>
                </SettingsSwitchContent>
                <FormControl>
                  <Checkbox
                    checked={field.value}
                    onCheckedChange={field.onChange}
                  />
                </FormControl>
              </SettingsSwitchItem>
            )}
          />

          <FormField
            control={form.control}
            name='traffic_control.country_header'
            render={({ field }) => (
              <FormItem className='max-w-md'>
                <FormLabel>{t('Country header')}</FormLabel>
                <FormControl>
                  <Input placeholder='CF-IPCountry' {...field} />
                </FormControl>
                <FormDescription>
                  {t(
                    'Trusted proxy or CDN header containing an ISO 3166-1 alpha-2 country code.'
                  )}
                </FormDescription>
                <FormMessage />
              </FormItem>
            )}
          />

          <FormField
            control={form.control}
            name='traffic_control.warning_title'
            render={({ field }) => (
              <FormItem className='max-w-md'>
                <FormLabel>{t('Warning page title')}</FormLabel>
                <FormControl>
                  <Input {...field} />
                </FormControl>
                <FormMessage />
              </FormItem>
            )}
          />

          <FormField
            control={form.control}
            name='traffic_control.warning_content'
            render={({ field }) => (
              <FormItem className='max-w-2xl'>
                <FormLabel>{t('Warning page content')}</FormLabel>
                <FormControl>
                  <Textarea rows={3} {...field} />
                </FormControl>
                <FormMessage />
              </FormItem>
            )}
          />

          <FormField
            control={form.control}
            name='traffic_control.warning_annotation'
            render={({ field }) => (
              <FormItem className='max-w-2xl'>
                <FormLabel>{t('Warning page note')}</FormLabel>
                <FormControl>
                  <Textarea rows={3} {...field} />
                </FormControl>
                <FormMessage />
              </FormItem>
            )}
          />
        </SettingsForm>
      </Form>
    </SettingsSection>
  )
}
