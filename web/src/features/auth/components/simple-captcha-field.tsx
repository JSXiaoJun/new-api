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
import { useId } from 'react'
import { useTranslation } from 'react-i18next'

import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import { cn } from '@/lib/utils'

interface SimpleCaptchaFieldProps {
  image: string
  value: string
  onValueChange: (value: string) => void
  onRefresh: () => void
  isLoading?: boolean
  disabled?: boolean
  className?: string
}

export function SimpleCaptchaField(props: SimpleCaptchaFieldProps) {
  const { t } = useTranslation()
  const inputId = useId()

  let refreshContent = (
    <img
      src={props.image}
      alt={t('Captcha image')}
      className='h-full w-full object-fill'
      draggable={false}
    />
  )
  if (props.isLoading || !props.image) {
    refreshContent = <Loader2 className='h-4 w-4 animate-spin' />
  }

  return (
    <div className={cn('grid gap-2', props.className)}>
      <Label htmlFor={inputId}>{t('Captcha')}</Label>
      <div className='flex items-center gap-2'>
        <Input
          id={inputId}
          className='flex-1'
          placeholder={t('Enter the characters in the image')}
          value={props.value}
          onChange={(event) => props.onValueChange(event.target.value)}
          autoComplete='off'
          autoCapitalize='characters'
          spellCheck={false}
          maxLength={8}
          disabled={props.disabled}
        />
        <Button
          type='button'
          variant='outline'
          className='h-9 w-[98px] shrink-0 overflow-hidden p-0'
          onClick={props.onRefresh}
          disabled={props.disabled || props.isLoading}
          aria-label={t('Refresh captcha')}
          title={t('Refresh captcha')}
        >
          {refreshContent}
        </Button>
      </div>
    </div>
  )
}
