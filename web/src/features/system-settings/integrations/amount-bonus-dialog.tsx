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
import { useEffect } from 'react'
import { useForm } from 'react-hook-form'
import { useTranslation } from 'react-i18next'
import * as z from 'zod'

import { Dialog } from '@/components/dialog'
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

import { MAX_TOP_UP_BONUS_PERCENT } from './amount-bonus'

const createAmountBonusDialogSchema = (t: (key: string) => string) =>
  z.object({
    amount: z
      .number()
      .positive(t('Amount must be greater than 0'))
      .int(t('Amount must be a whole number')),
    bonusPercent: z
      .number()
      .positive(t('Bonus percentage must be greater than 0'))
      .max(
        MAX_TOP_UP_BONUS_PERCENT,
        t('Bonus percentage must be at most 1000')
      ),
  })

type AmountBonusDialogFormValues = z.infer<
  ReturnType<typeof createAmountBonusDialogSchema>
>

const AMOUNT_BONUS_FORM_ID = 'amount-bonus-form'

export type AmountBonusData = {
  amount: number
  bonusPercent: number
}

type AmountBonusDialogProps = {
  open: boolean
  onOpenChange: (open: boolean) => void
  onSave: (data: AmountBonusData) => void
  editData?: AmountBonusData | null
}

export function AmountBonusDialog(props: AmountBonusDialogProps) {
  const { t } = useTranslation()
  const isEditMode = !!props.editData
  const form = useForm<AmountBonusDialogFormValues>({
    resolver: zodResolver(createAmountBonusDialogSchema(t)),
    defaultValues: { amount: 0, bonusPercent: 10 },
  })

  useEffect(() => {
    form.reset(props.editData ?? { amount: 0, bonusPercent: 10 })
  }, [props.editData, form, props.open])

  const handleSubmit = (values: AmountBonusDialogFormValues) => {
    props.onSave(values)
    form.reset()
    props.onOpenChange(false)
  }

  return (
    <Dialog
      open={props.open}
      onOpenChange={props.onOpenChange}
      title={isEditMode ? t('Edit bonus tier') : t('Add bonus tier')}
      description={t(
        'Grant extra balance as a percentage of the paid top-up once the amount reaches this threshold.'
      )}
      contentClassName='sm:max-w-[500px]'
      contentHeight='auto'
      bodyClassName='space-y-4'
      footer={
        <>
          <Button
            type='button'
            variant='outline'
            onClick={() => props.onOpenChange(false)}
          >
            {t('Cancel')}
          </Button>
          <Button type='submit' form={AMOUNT_BONUS_FORM_ID}>
            {isEditMode ? t('Update') : t('Add')}
          </Button>
        </>
      }
    >
      <Form {...form}>
        <form
          id={AMOUNT_BONUS_FORM_ID}
          onSubmit={form.handleSubmit(handleSubmit)}
          className='space-y-4'
        >
          <FormField
            control={form.control}
            name='amount'
            render={({ field }) => (
              <FormItem>
                <FormLabel>{t('Minimum recharge amount')}</FormLabel>
                <FormControl>
                  <Input
                    type='number'
                    step='1'
                    min='1'
                    placeholder={t('e.g., 100')}
                    {...field}
                    onChange={(e) =>
                      field.onChange(Number.parseInt(e.target.value, 10) || 0)
                    }
                    disabled={isEditMode}
                  />
                </FormControl>
                <FormDescription>
                  {isEditMode
                    ? t('Amount cannot be changed when editing.')
                    : t(
                        'Top-ups at or above this amount receive the bonus. The highest reached tier applies.'
                      )}
                </FormDescription>
                <FormMessage />
              </FormItem>
            )}
          />

          <FormField
            control={form.control}
            name='bonusPercent'
            render={({ field }) => (
              <FormItem>
                <FormLabel>{t('Bonus percentage (%)')}</FormLabel>
                <FormControl>
                  <Input
                    type='number'
                    step='0.1'
                    min='0.1'
                    max={MAX_TOP_UP_BONUS_PERCENT}
                    placeholder={t('e.g., 10')}
                    {...field}
                    onChange={(e) =>
                      field.onChange(Number.parseFloat(e.target.value) || 0)
                    }
                  />
                </FormControl>
                <FormDescription>
                  {t(
                    'For example, 10 means a 100 top-up receives an extra 10 in balance.'
                  )}
                </FormDescription>
                <FormMessage />
              </FormItem>
            )}
          />
        </form>
      </Form>
    </Dialog>
  )
}
