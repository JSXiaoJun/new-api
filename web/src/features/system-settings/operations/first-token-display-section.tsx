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
import { ArrowDown, ArrowUp, Plus, Trash2 } from 'lucide-react'
import { useMemo } from 'react'
import { useFieldArray, type Resolver } from 'react-hook-form'
import { useTranslation } from 'react-i18next'
import * as z from 'zod'

import { Alert, AlertDescription } from '@/components/ui/alert'
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
  FIRST_TOKEN_DISPLAY_OPTION_KEY,
  MAX_FIRST_TOKEN_DISPLAY_RULES,
  MAX_FIRST_TOKEN_DISPLAY_VALUE,
  parseFirstTokenDisplayConfig,
} from '../../usage-logs/lib/first-token-display'
import {
  SettingsForm,
  SettingsSwitchContent,
  SettingsSwitchItem,
} from '../components/settings-form-layout'
import { SettingsPageFormActions } from '../components/settings-page-context'
import { SettingsSection } from '../components/settings-section'
import { useSettingsForm } from '../hooks/use-settings-form'
import { useUpdateOption } from '../hooks/use-update-option'

const ruleSchema = z.object({
  id: z.string().trim().min(1).max(100),
  comparison: z.enum(['gt', 'gte']),
  threshold: z.coerce.number().min(0).max(MAX_FIRST_TOKEN_DISPLAY_VALUE),
  operation: z.enum(['add', 'subtract', 'multiply']),
  value: z.coerce.number().min(0).max(MAX_FIRST_TOKEN_DISPLAY_VALUE),
})

const schema = z.object({
  enabled: z.boolean(),
  rules: z.array(ruleSchema).max(MAX_FIRST_TOKEN_DISPLAY_RULES),
})
type FormValues = z.infer<typeof schema>

type Props = { defaultValue: string }

export function FirstTokenDisplaySection(props: Props) {
  const { t } = useTranslation()
  const updateOption = useUpdateOption()
  const defaults = useMemo(
    () => parseFirstTokenDisplayConfig(props.defaultValue),
    [props.defaultValue]
  )
  const { form, handleSubmit, handleReset, isSubmitting } =
    useSettingsForm<FormValues>({
      resolver: zodResolver(schema) as Resolver<FormValues>,
      defaultValues: defaults,
      onSubmit: async (values) => {
        await updateOption.mutateAsync({
          key: FIRST_TOKEN_DISPLAY_OPTION_KEY,
          value: JSON.stringify(values),
        })
      },
    })
  // Keep the persisted rule id separate from react-hook-form's internal row key.
  const fieldArray = useFieldArray({
    control: form.control,
    name: 'rules',
    keyName: '_key',
  })

  return (
    <SettingsSection title={t('First Token Display')}>
      <Alert>
        <AlertDescription>
          {t(
            'Control how first-token latency is displayed in usage logs. Recorded timing and billing data are not changed.'
          )}
        </AlertDescription>
      </Alert>
      <Form {...form}>
        <SettingsForm onSubmit={handleSubmit}>
          <SettingsPageFormActions
            onSave={handleSubmit}
            onReset={handleReset}
            isSaving={updateOption.isPending || isSubmitting}
          />
          <FormField
            control={form.control}
            name='enabled'
            render={({ field }) => (
              <SettingsSwitchItem>
                <SettingsSwitchContent>
                  <FormLabel>
                    {t('Enable first-token display adjustment')}
                  </FormLabel>
                  <FormDescription>
                    {t(
                      'Apply the first matching rule to the displayed first-token latency.'
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
          <div className='space-y-3 lg:col-span-2'>
            <div className='flex items-center justify-between gap-3'>
              <div>
                <h4 className='font-medium'>{t('Display adjustment rules')}</h4>
                <p className='text-muted-foreground text-xs'>
                  {t(
                    'Rules are evaluated from top to bottom. The first matching rule is applied.'
                  )}
                </p>
              </div>
              <Button
                type='button'
                size='sm'
                variant='outline'
                disabled={
                  fieldArray.fields.length >= MAX_FIRST_TOKEN_DISPLAY_RULES
                }
                onClick={() =>
                  fieldArray.append({
                    id: `rule-${Date.now()}`,
                    comparison: 'gte',
                    threshold: 0,
                    operation: 'subtract',
                    value: 0,
                  })
                }
              >
                <Plus data-icon='inline-start' />
                <span>{t('Add Rule')}</span>
              </Button>
            </div>
            {fieldArray.fields.map((item, index) => (
              <div
                key={item._key}
                className='grid gap-3 rounded-lg border p-3 md:grid-cols-[1.2fr_1fr_1fr_1fr_auto]'
              >
                <FormField
                  control={form.control}
                  name={`rules.${index}.id`}
                  render={({ field }) => (
                    <FormItem>
                      <FormLabel>{t('Name')}</FormLabel>
                      <FormControl>
                        <Input {...field} />
                      </FormControl>
                      <FormMessage />
                    </FormItem>
                  )}
                />
                <FormField
                  control={form.control}
                  name={`rules.${index}.comparison`}
                  render={({ field }) => (
                    <FormItem>
                      <FormLabel>{t('Condition')}</FormLabel>
                      <Select
                        value={field.value}
                        onValueChange={field.onChange}
                      >
                        <FormControl>
                          <SelectTrigger>
                            <SelectValue />
                          </SelectTrigger>
                        </FormControl>
                        <SelectContent>
                          <SelectGroup>
                            <SelectItem value='gt'>
                              {t('Greater than')}
                            </SelectItem>
                            <SelectItem value='gte'>
                              {t('Greater than or equal to')}
                            </SelectItem>
                          </SelectGroup>
                        </SelectContent>
                      </Select>
                      <FormMessage />
                    </FormItem>
                  )}
                />
                <FormField
                  control={form.control}
                  name={`rules.${index}.threshold`}
                  render={({ field }) => (
                    <FormItem>
                      <FormLabel>{t('Threshold (seconds)')}</FormLabel>
                      <FormControl>
                        <Input
                          type='number'
                          min={0}
                          max={MAX_FIRST_TOKEN_DISPLAY_VALUE}
                          step='any'
                          {...field}
                        />
                      </FormControl>
                      <FormMessage />
                    </FormItem>
                  )}
                />
                <FormField
                  control={form.control}
                  name={`rules.${index}.operation`}
                  render={({ field }) => (
                    <FormItem>
                      <FormLabel>{t('Adjustment')}</FormLabel>
                      <Select
                        value={field.value}
                        onValueChange={field.onChange}
                      >
                        <FormControl>
                          <SelectTrigger>
                            <SelectValue />
                          </SelectTrigger>
                        </FormControl>
                        <SelectContent>
                          <SelectGroup>
                            <SelectItem value='subtract'>
                              {t('Subtract')}
                            </SelectItem>
                            <SelectItem value='add'>{t('Add')}</SelectItem>
                            <SelectItem value='multiply'>
                              {t('Multiply')}
                            </SelectItem>
                          </SelectGroup>
                        </SelectContent>
                      </Select>
                      <FormMessage />
                    </FormItem>
                  )}
                />
                <div className='flex items-end gap-1'>
                  <FormField
                    control={form.control}
                    name={`rules.${index}.value`}
                    render={({ field }) => (
                      <FormItem>
                        <FormLabel>{t('Value')}</FormLabel>
                        <FormControl>
                          <Input
                            type='number'
                            min={0}
                            max={MAX_FIRST_TOKEN_DISPLAY_VALUE}
                            step='any'
                            {...field}
                          />
                        </FormControl>
                        <FormMessage />
                      </FormItem>
                    )}
                  />
                  <Button
                    type='button'
                    variant='ghost'
                    size='icon-sm'
                    title={t('Move up')}
                    aria-label={t('Move up')}
                    disabled={index === 0}
                    onClick={() => fieldArray.move(index, index - 1)}
                  >
                    <ArrowUp />
                  </Button>
                  <Button
                    type='button'
                    variant='ghost'
                    size='icon-sm'
                    title={t('Move down')}
                    aria-label={t('Move down')}
                    disabled={index === fieldArray.fields.length - 1}
                    onClick={() => fieldArray.move(index, index + 1)}
                  >
                    <ArrowDown />
                  </Button>
                  <Button
                    type='button'
                    variant='ghost'
                    size='icon-sm'
                    title={t('Delete')}
                    aria-label={t('Delete')}
                    onClick={() => fieldArray.remove(index)}
                  >
                    <Trash2 />
                  </Button>
                </div>
              </div>
            ))}
            {fieldArray.fields.length === 0 && (
              <p className='text-muted-foreground rounded-lg border border-dashed p-4 text-sm'>
                {t('No display adjustment rules.')}
              </p>
            )}
          </div>
        </SettingsForm>
      </Form>
    </SettingsSection>
  )
}
