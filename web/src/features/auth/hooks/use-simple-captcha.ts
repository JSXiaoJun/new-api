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
import { useQuery } from '@tanstack/react-query'
import i18next from 'i18next'
import { useState } from 'react'
import { toast } from 'sonner'

import { getSimpleCaptcha } from '@/features/auth/api'
import type { SimpleCaptchaAnswer } from '@/features/auth/types'
import { useStatus } from '@/hooks/use-status'

// The server keeps a captcha for five minutes; fetch a new one before that.
const SIMPLE_CAPTCHA_REFRESH_MS = 4 * 60 * 1000

/**
 * Hook for the image captcha on the sign-in and sign-up forms. Each captcha
 * allows one attempt, so callers refresh it after every submission.
 */
export function useSimpleCaptcha() {
  const { status } = useStatus()
  const isSimpleCaptchaEnabled = Boolean(status?.simple_captcha_check)
  // The typed answer belongs to the image it was read from; a new image
  // starts with an empty answer.
  const [answer, setAnswer] = useState({ captchaId: '', code: '' })

  const captchaQuery = useQuery({
    queryKey: ['auth', 'simple-captcha'],
    queryFn: getSimpleCaptcha,
    enabled: isSimpleCaptchaEnabled,
    gcTime: 0,
    refetchInterval: SIMPLE_CAPTCHA_REFRESH_MS,
    refetchOnWindowFocus: false,
    retry: false,
  })

  const captchaId = captchaQuery.data?.captcha_id ?? ''
  const captchaCode = answer.captchaId === captchaId ? answer.code : ''

  const setCaptchaCode = (code: string) => setAnswer({ captchaId, code })

  const refreshCaptcha = () => {
    if (!isSimpleCaptchaEnabled) return
    setAnswer({ captchaId: '', code: '' })
    void captchaQuery.refetch()
  }

  /**
   * Validate that an answer was typed when the captcha is required
   */
  const validateCaptcha = (): boolean => {
    if (isSimpleCaptchaEnabled && (!captchaId || !captchaCode.trim())) {
      toast.error(i18next.t('Please enter the captcha'))
      return false
    }
    return true
  }

  const captchaAnswer: SimpleCaptchaAnswer | undefined = isSimpleCaptchaEnabled
    ? { captcha_id: captchaId, captcha_code: captchaCode.trim() }
    : undefined

  return {
    isSimpleCaptchaEnabled,
    captchaImage: captchaQuery.data?.image ?? '',
    isCaptchaLoading: captchaQuery.isFetching,
    captchaCode,
    setCaptchaCode,
    refreshCaptcha,
    validateCaptcha,
    captchaAnswer,
  }
}
