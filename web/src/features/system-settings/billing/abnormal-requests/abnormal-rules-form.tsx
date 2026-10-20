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
import { useQueryClient } from '@tanstack/react-query'
import i18next from 'i18next'
import type { Resolver } from 'react-hook-form'
import { useTranslation } from 'react-i18next'
import * as z from 'zod'

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
import { formatQuotaWithCurrency } from '@/lib/currency'

import { FormDirtyIndicator } from '../../components/form-dirty-indicator'
import {
  SettingsForm,
  SettingsFormGrid,
} from '../../components/settings-form-layout'
import { SettingsPageFormActions } from '../../components/settings-page-context'
import { useSettingsForm } from '../../hooks/use-settings-form'
import { useUpdateOption } from '../../hooks/use-update-option'

const integerInRange = (min: number, max: number) =>
  z.coerce
    .number({ error: () => i18next.t('Please enter a valid number') })
    .int({ error: () => i18next.t('Please enter a valid number') })
    .min(min, {
      error: () =>
        i18next.t('Must be greater than or equal to {{min}}', { min }),
    })
    .max(max, {
      error: () => i18next.t('Must be less than or equal to {{max}}', { max }),
    })

const rulesSchema = z.object({
  abnormal_request_setting: z.object({
    flag_hourly_count: integerInRange(1, 1_000_000),
    flag_ratio_percent: integerInRange(1, 100),
    flag_min_requests: integerInRange(1, 1_000_000),
    flag_forgone_quota: integerInRange(1, 1_000_000_000),
  }),
})

export type AbnormalRulesValues = z.infer<typeof rulesSchema>

const FIELDS: Array<{
  name: keyof AbnormalRulesValues['abnormal_request_setting']
  labelKey: string
  descriptionKey: string
}> = [
  {
    name: 'flag_hourly_count',
    labelKey: 'Abnormal requests within one hour',
    descriptionKey:
      'Counts requests the user caused to end abnormally in the busiest hour; a request that already received output counts twice.',
  },
  {
    name: 'flag_ratio_percent',
    labelKey: 'Abnormal share within one hour (%)',
    descriptionKey:
      'Share of abnormal requests among all requests of the busiest hour. Needs consume logging.',
  },
  {
    name: 'flag_min_requests',
    labelKey: 'Minimum requests for the share rule',
    descriptionKey:
      'The share rule only applies to an hour with at least this many requests.',
  },
  {
    name: 'flag_forgone_quota',
    labelKey: 'Uncollected amount in the period (quota)',
    descriptionKey:
      'Total charge the user avoided in the reviewed period. Currently {{amount}}.',
  },
]

export function AbnormalRulesForm(props: {
  defaultValues: AbnormalRulesValues
}) {
  const { t } = useTranslation()
  const updateOption = useUpdateOption()
  const queryClient = useQueryClient()
  const { form, handleSubmit, isDirty, isSubmitting } =
    useSettingsForm<AbnormalRulesValues>({
      resolver: zodResolver(rulesSchema) as Resolver<
        AbnormalRulesValues,
        unknown,
        AbnormalRulesValues
      >,
      defaultValues: props.defaultValues,
      onSubmit: async (_data, changedFields) => {
        for (const [key, value] of Object.entries(changedFields)) {
          await updateOption.mutateAsync({ key, value: String(value) })
        }
        // The flags in the user list are computed with these rules.
        await queryClient.invalidateQueries({
          queryKey: ['abnormal-charge-users'],
        })
      },
    })

  return (
    <Form {...form}>
      <SettingsForm onSubmit={handleSubmit}>
        <SettingsPageFormActions
          onSave={handleSubmit}
          isSaving={updateOption.isPending || isSubmitting}
          saveLabel='Save abnormal request rules'
        />
        <FormDirtyIndicator isDirty={isDirty} />
        <p className='text-muted-foreground text-sm'>
          {t(
            'A user is marked suspicious when any two of the three rules match. Marking never charges anyone; review the records and recover charges yourself.'
          )}
        </p>
        <SettingsFormGrid>
          {FIELDS.map((item) => (
            <FormField
              key={item.name}
              control={form.control}
              name={`abnormal_request_setting.${item.name}`}
              render={({ field }) => (
                <FormItem>
                  <FormLabel>{t(item.labelKey)}</FormLabel>
                  <FormControl>
                    <Input
                      type='number'
                      min={1}
                      step={1}
                      value={field.value ?? ''}
                      onChange={(event) => {
                        const value = event.currentTarget.valueAsNumber
                        field.onChange(Number.isNaN(value) ? '' : value)
                      }}
                      name={field.name}
                      onBlur={field.onBlur}
                      ref={field.ref}
                    />
                  </FormControl>
                  <FormDescription>
                    {t(item.descriptionKey, {
                      amount: formatQuotaWithCurrency(Number(field.value) || 0),
                    })}
                  </FormDescription>
                  <FormMessage />
                </FormItem>
              )}
            />
          ))}
        </SettingsFormGrid>
      </SettingsForm>
    </Form>
  )
}
