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
import { Loader2 } from 'lucide-react'
import { useState } from 'react'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'

import { Button } from '@/components/ui/button'
import { Checkbox } from '@/components/ui/checkbox'
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from '@/components/ui/dialog'
import { Input } from '@/components/ui/input'
import { formatTimestampToDate } from '@/lib/format'

import { confirmPurchaseAgreement, isApiSuccess } from '../../api'

interface PurchaseAgreementDialogProps {
  open: boolean
  onOpenChange: (open: boolean) => void
  /** Unix seconds when the user signed; 0 or undefined means unsigned */
  signedAt?: number
  onConfirmed: (signedAt: number) => void
}

export function PurchaseAgreementDialog(props: PurchaseAgreementDialogProps) {
  const { t } = useTranslation()
  const [notMainlandCitizen, setNotMainlandCitizen] = useState(false)
  const [notInMainland, setNotInMainland] = useState(false)
  const [statement, setStatement] = useState('')
  const [submitting, setSubmitting] = useState(false)

  const signed = (props.signedAt ?? 0) > 0
  const phrase = t('I understand and confirm')
  const trimmedStatement = statement.trim()
  const statementMismatch =
    !signed && trimmedStatement !== '' && trimmedStatement !== phrase
  const canSubmit =
    notMainlandCitizen && notInMainland && trimmedStatement === phrase

  const items = [
    {
      id: 'not-mainland-citizen',
      label: t(
        'I confirm that I am not a citizen of mainland China or an entity within mainland China.'
      ),
      checked: notMainlandCitizen,
      onChange: setNotMainlandCitizen,
    },
    {
      id: 'not-in-mainland',
      label: t(
        'I confirm that my current location is not in mainland China, and I voluntarily continue with the purchase.'
      ),
      checked: notInMainland,
      onChange: setNotInMainland,
    },
  ]

  const handleSubmit = async () => {
    if (!canSubmit || submitting) return
    setSubmitting(true)
    try {
      const response = await confirmPurchaseAgreement({
        confirm_not_mainland_citizen: notMainlandCitizen,
        confirm_not_in_mainland: notInMainland,
        statement: trimmedStatement,
      })
      const signedAt = response.data?.purchase_agreement_at ?? 0
      if (!isApiSuccess(response) || signedAt <= 0) {
        toast.error(response.message || t('Confirmation failed'))
        return
      }
      toast.success(t('Pre-purchase confirmation completed'))
      props.onConfirmed(signedAt)
    } catch {
      toast.error(t('Confirmation failed'))
    } finally {
      setSubmitting(false)
    }
  }

  return (
    <Dialog open={props.open} onOpenChange={props.onOpenChange}>
      <DialogContent className='max-sm:w-[calc(100vw-1.5rem)] sm:max-w-lg'>
        <DialogHeader>
          <DialogTitle className='text-lg font-semibold'>
            {t('Pre-purchase Confirmation')}
          </DialogTitle>
          <DialogDescription>
            {t(
              'This service is not provided to individuals or organizations in mainland China. Before continuing, please confirm the following:'
            )}
          </DialogDescription>
        </DialogHeader>

        <div className='space-y-3'>
          {items.map((item) => (
            <label
              key={item.id}
              className='bg-muted/50 flex cursor-pointer items-start gap-3 rounded-lg border p-3 text-sm leading-5 has-[[data-disabled]]:cursor-default'
            >
              <Checkbox
                checked={signed || item.checked}
                disabled={signed || submitting}
                onCheckedChange={(value) => item.onChange(value === true)}
                className='mt-0.5'
              />
              <span>{item.label}</span>
            </label>
          ))}

          <div className='space-y-1.5'>
            <Input
              aria-label={t('Confirmation text')}
              placeholder={t('Please enter "{{phrase}}"', { phrase })}
              value={signed ? phrase : statement}
              onChange={(e) => setStatement(e.target.value)}
              disabled={signed || submitting}
              aria-invalid={statementMismatch}
              maxLength={64}
              autoComplete='off'
            />
            {statementMismatch && (
              <p className='text-destructive text-xs'>
                {t('The text you entered does not match')}
              </p>
            )}
          </div>

          <p className='text-muted-foreground text-xs'>
            {t(
              'By continuing, you declare that you are not a citizen or resident of any region restricted by this service.'
            )}
          </p>
          {signed && (
            <p className='text-muted-foreground text-xs'>
              {t('Confirmed at {{time}}', {
                time: formatTimestampToDate(props.signedAt),
              })}
            </p>
          )}
        </div>

        <DialogFooter>
          {signed ? (
            <Button variant='outline' onClick={() => props.onOpenChange(false)}>
              {t('Close')}
            </Button>
          ) : (
            <Button onClick={handleSubmit} disabled={!canSubmit || submitting}>
              {submitting && <Loader2 className='mr-2 h-4 w-4 animate-spin' />}
              {t('Confirm and Continue')}
            </Button>
          )}
        </DialogFooter>
      </DialogContent>
    </Dialog>
  )
}
