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
import { Plus, Trash2 } from 'lucide-react'
import { useId, useLayoutEffect, useRef, useState } from 'react'
import { useTranslation } from 'react-i18next'

import { Button } from '@/components/ui/button'
import { Field, FieldLabel } from '@/components/ui/field'
import { Input } from '@/components/ui/input'
import { NativeSelect, NativeSelectOption } from '@/components/ui/native-select'
import {
  Tooltip,
  TooltipContent,
  TooltipTrigger,
} from '@/components/ui/tooltip'
import {
  combineBillingExpr,
  splitBillingExprAndRequestRules,
} from '@/features/pricing/lib/billing-expr'

import type { PeakPricing, PeakTariff } from './peak-pricing'
import { TieredPricingEditor } from './tiered-pricing-editor'

const tokenFields = [
  ['input_price', 'Input price'],
  ['output_price', 'Completion price'],
  ['cache_price', 'Cache read price'],
  ['create_cache_price', 'Cache write price'],
  ['image_price', 'Image input price'],
  ['audio_price', 'Audio input price'],
  ['audio_output_price', 'Audio output price'],
] as const

function TariffEditor(props: {
  value: PeakTariff
  onChange: (value: PeakTariff) => void
  modelName: string
}) {
  const { t } = useTranslation()
  const id = useId()
  const tariff = props.value
  const expression = splitBillingExprAndRequestRules(tariff.expression ?? '')
  const expressionDraft = useRef(expression)
  useLayoutEffect(() => {
    expressionDraft.current = splitBillingExprAndRequestRules(
      tariff.expression ?? ''
    )
  }, [tariff.expression])
  return (
    <div className='space-y-3'>
      <Field>
        <FieldLabel htmlFor={`${id}-mode`}>{t('Billing mode')}</FieldLabel>
        <NativeSelect
          id={`${id}-mode`}
          value={tariff.mode}
          onChange={(event) =>
            props.onChange({
              ...tariff,
              mode: event.target.value as PeakTariff['mode'],
            })
          }
        >
          <NativeSelectOption value='per_token'>
            {t('Per-token')}
          </NativeSelectOption>
          <NativeSelectOption value='per_request'>
            {t('Per-request')}
          </NativeSelectOption>
          <NativeSelectOption value='per_second'>
            {t('Per-second')}
          </NativeSelectOption>
          <NativeSelectOption value='tiered_expr'>
            {t('Expression')}
          </NativeSelectOption>
        </NativeSelect>
      </Field>
      {tariff.mode === 'per_token' && (
        <div className='grid grid-cols-1 gap-3 sm:grid-cols-2'>
          {tokenFields.map(([field, label]) => (
            <Field key={field}>
              <FieldLabel htmlFor={`${id}-${field}`}>
                {t(label)} ($/1M)
              </FieldLabel>
              <Input
                id={`${id}-${field}`}
                type='number'
                min='0'
                step='any'
                value={tariff[field] ?? ''}
                onChange={(event) =>
                  props.onChange({
                    ...tariff,
                    [field]:
                      event.target.value === ''
                        ? undefined
                        : Number(event.target.value),
                  })
                }
              />
            </Field>
          ))}
        </div>
      )}
      {(tariff.mode === 'per_request' || tariff.mode === 'per_second') && (
        <Field>
          <FieldLabel htmlFor={`${id}-price`}>
            {tariff.mode === 'per_second'
              ? t('Price per second')
              : t('Fixed price')}{' '}
            ($)
          </FieldLabel>
          <Input
            id={`${id}-price`}
            type='number'
            min='0'
            step='any'
            value={tariff.price ?? ''}
            onChange={(event) =>
              props.onChange({
                ...tariff,
                price:
                  event.target.value === ''
                    ? undefined
                    : Number(event.target.value),
              })
            }
          />
        </Field>
      )}
      {tariff.mode === 'tiered_expr' && (
        <TieredPricingEditor
          modelName={props.modelName}
          billingExpr={expression.billingExpr}
          requestRuleExpr={expression.requestRuleExpr}
          onBillingExprChange={(next) => {
            expressionDraft.current.billingExpr = next
            props.onChange({
              ...tariff,
              expression: combineBillingExpr(
                next,
                expressionDraft.current.requestRuleExpr
              ),
            })
          }}
          onRequestRuleExprChange={(next) => {
            expressionDraft.current.requestRuleExpr = next
            props.onChange({
              ...tariff,
              expression: combineBillingExpr(
                expressionDraft.current.billingExpr,
                next
              ),
            })
          }}
        />
      )}
    </div>
  )
}

export function PeakPricingEditor(props: {
  value: PeakPricing
  onChange: (value: PeakPricing) => void
  modelName: string
}) {
  const { t } = useTranslation()
  const id = useId()
  const nextPeriodKey = useRef(props.value.periods.length)
  const [periodKeys, setPeriodKeys] = useState(() =>
    props.value.periods.map((_, index) => index)
  )
  return (
    <div className='min-w-0 space-y-5'>
      <Field>
        <FieldLabel htmlFor={`${id}-timezone`}>{t('Timezone')}</FieldLabel>
        <Input
          id={`${id}-timezone`}
          value={props.value.timezone}
          onChange={(event) =>
            props.onChange({ ...props.value, timezone: event.target.value })
          }
        />
      </Field>
      <section
        className='space-y-3 border-t pt-4'
        aria-label={t('Default pricing')}
      >
        <h4 className='text-sm font-medium'>{t('Default pricing')}</h4>
        <TariffEditor
          value={props.value.default}
          onChange={(value) =>
            props.onChange({ ...props.value, default: value })
          }
          modelName={props.modelName}
        />
      </section>
      {props.value.periods.map((period, index) => (
        <section
          key={periodKeys[index]}
          className='space-y-3 border-t pt-4'
          aria-label={t('Time period {{number}}', { number: index + 1 })}
        >
          <div className='flex items-center justify-between gap-2'>
            <h4 className='text-sm font-medium'>
              {t('Time period {{number}}', { number: index + 1 })}
            </h4>
            <Tooltip>
              <TooltipTrigger
                render={
                  <Button
                    type='button'
                    variant='ghost'
                    size='icon'
                    aria-label={t('Delete time period')}
                    onClick={() => {
                      setPeriodKeys((keys) =>
                        keys.filter((_, i) => i !== index)
                      )
                      props.onChange({
                        ...props.value,
                        periods: props.value.periods.filter(
                          (_, i) => i !== index
                        ),
                      })
                    }}
                  >
                    <Trash2 className='size-4' />
                  </Button>
                }
              />
              <TooltipContent>{t('Delete time period')}</TooltipContent>
            </Tooltip>
          </div>
          <div className='grid grid-cols-2 gap-3'>
            {(['start', 'end'] as const).map((field) => (
              <Field key={field}>
                <FieldLabel htmlFor={`${id}-${index}-${field}`}>
                  {field === 'start' ? t('Start time') : t('End time')}
                </FieldLabel>
                <Input
                  className='min-w-0'
                  id={`${id}-${index}-${field}`}
                  type='time'
                  value={period[field]}
                  onChange={(event) =>
                    props.onChange({
                      ...props.value,
                      periods: props.value.periods.map((item, i) =>
                        i === index
                          ? { ...item, [field]: event.target.value }
                          : item
                      ),
                    })
                  }
                />
              </Field>
            ))}
          </div>
          <TariffEditor
            value={period.tariff}
            onChange={(tariff) =>
              props.onChange({
                ...props.value,
                periods: props.value.periods.map((item, i) =>
                  i === index ? { ...item, tariff } : item
                ),
              })
            }
            modelName={props.modelName}
          />
        </section>
      ))}
      <Button
        type='button'
        variant='outline'
        disabled={props.value.periods.length >= 48}
        onClick={() => {
          const key = nextPeriodKey.current++
          setPeriodKeys((keys) => [...keys, key])
          props.onChange({
            ...props.value,
            periods: [
              ...props.value.periods,
              { start: '18:00', end: '22:00', tariff: { mode: 'per_request' } },
            ],
          })
        }}
      >
        <Plus data-icon='inline-start' />
        {t('Add time period')}
      </Button>
    </div>
  )
}
