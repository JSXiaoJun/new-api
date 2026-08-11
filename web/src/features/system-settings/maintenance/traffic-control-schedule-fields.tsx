import { Plus, Trash2 } from 'lucide-react'
import { useFieldArray, useFormContext } from 'react-hook-form'
import { useTranslation } from 'react-i18next'

import { Button } from '@/components/ui/button'
import {
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
  SettingsSwitchContent,
  SettingsSwitchItem,
} from '../components/settings-form-layout'
import type { TrafficControlFormValues } from './traffic-control-schedule'

export function TrafficControlScheduleFields() {
  const { t } = useTranslation()
  const form = useFormContext<TrafficControlFormValues>()
  const timeRanges = useFieldArray({
    control: form.control,
    name: 'schedule.time_ranges',
  })
  const enabled = form.watch('schedule.enabled')
  const dailyRepeat = form.watch('schedule.daily_repeat')
  const rangeError = form.formState.errors.schedule?.time_ranges
  const rangeErrorMessage =
    rangeError && 'message' in rangeError
      ? String(rangeError.message ?? '')
      : ''

  return (
    <section
      className='border-border/70 grid min-w-0 gap-x-5 gap-y-5 border-y py-5 lg:grid-cols-2'
      data-settings-form-span='full'
      aria-label={t('Automatic traffic control')}
    >
      <FormField
        control={form.control}
        name='schedule.enabled'
        render={({ field }) => (
          <SettingsSwitchItem className='py-0 lg:col-span-2'>
            <SettingsSwitchContent>
              <FormLabel>{t('Automatic traffic control')}</FormLabel>
              <FormDescription>
                {t(
                  'Turn blocking on during the configured time periods and off outside them.'
                )}
              </FormDescription>
            </SettingsSwitchContent>
            <FormControl>
              <Switch checked={field.value} onCheckedChange={field.onChange} />
            </FormControl>
          </SettingsSwitchItem>
        )}
      />

      <FormField
        control={form.control}
        name='schedule.daily_repeat'
        render={({ field }) => (
          <SettingsSwitchItem className='py-0 lg:col-span-2'>
            <SettingsSwitchContent>
              <FormLabel>{t('Repeat every day')}</FormLabel>
              <FormDescription>
                {t(
                  'Repeat these time periods every day. Turn it off to run them only on the selected date.'
                )}
              </FormDescription>
            </SettingsSwitchContent>
            <FormControl>
              <Switch checked={field.value} onCheckedChange={field.onChange} />
            </FormControl>
          </SettingsSwitchItem>
        )}
      />

      {!dailyRepeat && (
        <FormField
          control={form.control}
          name='schedule.date'
          render={({ field }) => (
            <FormItem>
              <FormLabel>{t('Schedule date')}</FormLabel>
              <FormControl>
                <Input type='date' {...field} />
              </FormControl>
              <FormMessage />
            </FormItem>
          )}
        />
      )}

      <FormField
        control={form.control}
        name='schedule.timezone'
        render={({ field }) => (
          <FormItem>
            <FormLabel>{t('Schedule timezone')}</FormLabel>
            <FormControl>
              <Input placeholder='Asia/Shanghai' {...field} />
            </FormControl>
            <FormDescription>
              {t('Use an IANA timezone name for all scheduled times.')}
            </FormDescription>
            <FormMessage />
          </FormItem>
        )}
      />

      <div className='min-w-0 space-y-3 lg:col-span-2'>
        <div className='flex flex-wrap items-center justify-between gap-2'>
          <div className='space-y-0.5'>
            <h3 className='text-sm font-medium'>{t('Time periods')}</h3>
            <p className='text-muted-foreground text-sm'>
              {t('A time period may run across midnight.')}
            </p>
          </div>
          <Button
            type='button'
            variant='outline'
            size='sm'
            onClick={() =>
              timeRanges.append({ start_time: '09:00', end_time: '18:00' })
            }
            disabled={timeRanges.fields.length >= 24}
          >
            <Plus data-icon='inline-start' />
            {t('Add time period')}
          </Button>
        </div>

        {timeRanges.fields.map((timeRange, index) => (
          <div
            key={timeRange.id}
            className='grid min-w-0 grid-cols-[minmax(0,1fr)_minmax(0,1fr)_auto] items-start gap-2'
          >
            <FormField
              control={form.control}
              name={`schedule.time_ranges.${index}.start_time`}
              render={({ field }) => (
                <FormItem>
                  <FormLabel>{t('Start time')}</FormLabel>
                  <FormControl>
                    <Input type='time' {...field} />
                  </FormControl>
                  <FormMessage />
                </FormItem>
              )}
            />
            <FormField
              control={form.control}
              name={`schedule.time_ranges.${index}.end_time`}
              render={({ field }) => (
                <FormItem>
                  <FormLabel>{t('End time')}</FormLabel>
                  <FormControl>
                    <Input type='time' {...field} />
                  </FormControl>
                  <FormMessage />
                </FormItem>
              )}
            />
            <Button
              type='button'
              variant='ghost'
              size='icon'
              className='mt-6'
              onClick={() => timeRanges.remove(index)}
              aria-label={t('Remove time period')}
              title={t('Remove time period')}
            >
              <Trash2 />
            </Button>
          </div>
        ))}

        {rangeErrorMessage ? (
          <p className='text-destructive text-sm'>{rangeErrorMessage}</p>
        ) : null}
        {!enabled ? (
          <p className='text-muted-foreground text-xs'>
            {t(
              'The saved schedule will not apply until automation is enabled.'
            )}
          </p>
        ) : null}
      </div>
    </section>
  )
}
