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
import { Check, X } from 'lucide-react'
import { useMemo } from 'react'
import { useForm, type Resolver } from 'react-hook-form'
import { useTranslation } from 'react-i18next'

import { MultiSelect } from '@/components/multi-select'
import { Button } from '@/components/ui/button'
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
import {
  buildDefaultDiscountWindow,
  createDiscountSettingsSchema,
  getDiscountGroupOptions,
  parseDiscountSchedule,
  type DiscountSettingsValues,
} from './discount-schedule'

type DiscountSettingsSectionProps = {
  scheduleValue: string
  groupRatioValue: string
  usableGroupsValue: string
}

export function DiscountSettingsSection(props: DiscountSettingsSectionProps) {
  const { t } = useTranslation()
  const updateOption = useUpdateOption()
  const schedule = useMemo(
    () => parseDiscountSchedule(props.scheduleValue),
    [props.scheduleValue]
  )
  const defaultWindow = useMemo(
    () => buildDefaultDiscountWindow(new Date()),
    []
  )
  const groupOptions = useMemo(
    () =>
      getDiscountGroupOptions(props.groupRatioValue, props.usableGroupsValue),
    [props.groupRatioValue, props.usableGroupsValue]
  )
  const schema = useMemo(() => createDiscountSettingsSchema(t), [t])
  const form = useForm<DiscountSettingsValues>({
    resolver: zodResolver(schema) as Resolver<DiscountSettingsValues>,
    defaultValues: {
      ...schedule,
      start_at: schedule.start_at || defaultWindow.startAt,
      end_at: schedule.end_at || defaultWindow.endAt,
    },
  })

  const enabled = form.watch('enabled')
  const dailyRepeat = form.watch('daily_repeat')
  const ratio = form.watch('ratio')
  const selectedGroups = form.watch('groups')
  const { isDirty, isSubmitting } = form.formState

  async function onSubmit(values: DiscountSettingsValues) {
    const response = await updateOption.mutateAsync({
      key: 'discount_setting.schedule',
      value: JSON.stringify(values),
    })
    if (response.success) {
      form.reset(values)
    }
  }

  return (
    <SettingsSection title={t('Discount Settings')}>
      <Form {...form}>
        <SettingsForm onSubmit={form.handleSubmit(onSubmit)} autoComplete='off'>
          <SettingsPageFormActions
            onSave={form.handleSubmit(onSubmit)}
            isSaving={updateOption.isPending || isSubmitting}
            isSaveDisabled={!isDirty}
            saveLabel='Save discount settings'
          />

          <FormField
            control={form.control}
            name='enabled'
            render={({ field }) => (
              <SettingsSwitchItem>
                <SettingsSwitchContent>
                  <FormLabel>
                    {t('Enable scheduled billing discount')}
                  </FormLabel>
                  <FormDescription>
                    {t(
                      'Apply the configured multiplier only to requests from selected user groups.'
                    )}
                  </FormDescription>
                </SettingsSwitchContent>
                <FormControl>
                  <Switch
                    checked={field.value}
                    onCheckedChange={field.onChange}
                    disabled={updateOption.isPending || isSubmitting}
                  />
                </FormControl>
              </SettingsSwitchItem>
            )}
          />

          <FormField
            control={form.control}
            name='ratio'
            render={({ field }) => (
              <FormItem>
                <FormLabel>{t('Billing multiplier')}</FormLabel>
                <FormControl>
                  <Input
                    type='number'
                    min='0.01'
                    max='1'
                    step='0.01'
                    {...field}
                  />
                </FormControl>
                <FormDescription>
                  {t(
                    '1 means no discount. For example, 0.8 charges 80% of the normal price.'
                  )}
                  {typeof ratio === 'number' && ratio > 0 && ratio <= 1
                    ? ` ${t('{{percent}}% off', {
                        percent: Math.round((1 - ratio) * 100),
                      })}`
                    : ''}
                </FormDescription>
                <FormMessage />
              </FormItem>
            )}
          />

          <FormField
            control={form.control}
            name='timezone'
            render={({ field }) => (
              <FormItem>
                <FormLabel>{t('Schedule timezone')}</FormLabel>
                <FormControl>
                  <Input placeholder='Asia/Shanghai' {...field} />
                </FormControl>
                <FormDescription>
                  {t('Use an IANA timezone name for all discount times.')}
                </FormDescription>
                <FormMessage />
              </FormItem>
            )}
          />

          <FormField
            control={form.control}
            name='groups'
            render={({ field }) => (
              <FormItem data-settings-form-span='full'>
                <div className='flex flex-wrap items-center justify-between gap-2'>
                  <FormLabel>{t('Discounted user groups')}</FormLabel>
                  <div className='flex flex-wrap gap-2'>
                    <Button
                      type='button'
                      variant='outline'
                      size='sm'
                      onClick={() =>
                        field.onChange(groupOptions.map((item) => item.value))
                      }
                      disabled={groupOptions.length === 0}
                    >
                      <Check data-icon='inline-start' />
                      {t('Select all')}
                    </Button>
                    <Button
                      type='button'
                      variant='outline'
                      size='sm'
                      onClick={() => field.onChange([])}
                      disabled={selectedGroups.length === 0}
                    >
                      <X data-icon='inline-start' />
                      {t('Clear selection')}
                    </Button>
                  </div>
                </div>
                <FormControl>
                  <MultiSelect
                    options={groupOptions}
                    selected={field.value}
                    onChange={field.onChange}
                    placeholder={t('Select discounted user groups...')}
                    emptyText={t('No user groups available')}
                    maxVisibleChips={8}
                  />
                </FormControl>
                <FormDescription>
                  {t(
                    'The discount matches the user account group, regardless of the API token routing group.'
                  )}
                </FormDescription>
                <FormMessage />
              </FormItem>
            )}
          />

          <FormField
            control={form.control}
            name='daily_repeat'
            render={({ field }) => (
              <SettingsSwitchItem>
                <SettingsSwitchContent>
                  <FormLabel>{t('Repeat every day')}</FormLabel>
                  <FormDescription>
                    {t(
                      'Use the same daily time window. A start time later than the end time runs across midnight.'
                    )}
                  </FormDescription>
                </SettingsSwitchContent>
                <FormControl>
                  <Switch
                    checked={field.value}
                    onCheckedChange={field.onChange}
                    disabled={updateOption.isPending || isSubmitting}
                  />
                </FormControl>
              </SettingsSwitchItem>
            )}
          />

          {dailyRepeat ? (
            <>
              <FormField
                control={form.control}
                name='start_time'
                render={({ field }) => (
                  <FormItem>
                    <FormLabel>{t('Daily start time')}</FormLabel>
                    <FormControl>
                      <Input type='time' {...field} />
                    </FormControl>
                    <FormMessage />
                  </FormItem>
                )}
              />
              <FormField
                control={form.control}
                name='end_time'
                render={({ field }) => (
                  <FormItem>
                    <FormLabel>{t('Daily end time')}</FormLabel>
                    <FormControl>
                      <Input type='time' {...field} />
                    </FormControl>
                    <FormMessage />
                  </FormItem>
                )}
              />
            </>
          ) : (
            <>
              <FormField
                control={form.control}
                name='start_at'
                render={({ field }) => (
                  <FormItem>
                    <FormLabel>{t('Start date and time')}</FormLabel>
                    <FormControl>
                      <Input type='datetime-local' {...field} />
                    </FormControl>
                    <FormMessage />
                  </FormItem>
                )}
              />
              <FormField
                control={form.control}
                name='end_at'
                render={({ field }) => (
                  <FormItem>
                    <FormLabel>{t('End date and time')}</FormLabel>
                    <FormControl>
                      <Input type='datetime-local' {...field} />
                    </FormControl>
                    <FormMessage />
                  </FormItem>
                )}
              />
            </>
          )}

          {!enabled && (
            <p
              className='text-muted-foreground text-xs'
              data-settings-form-span='full'
            >
              {t(
                'The saved schedule will not affect billing until it is enabled.'
              )}
            </p>
          )}
        </SettingsForm>
      </Form>
    </SettingsSection>
  )
}
