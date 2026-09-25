/*
Copyright (C) 2023-2026 QuantumNous

This program is free software: you can redistribute it and/or modify
it under the terms of the GNU Affero General Public License as published by
the Free Software Foundation, either version 3 of the License, or
(at your option) any later version.

This program is distributed in the hope that it will be useful,
but WITHOUT ANY WARRANTY; without even the implied warranty of
MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE. See the
GNU Affero General Public License for more details.

You should have received a copy of the GNU Affero General Public License
along with this program. If not, see <https://www.gnu.org/licenses/>.

For commercial licensing, please contact support@quantumnous.com
*/
import { Ban, Check, Copy, Loader2, RefreshCw } from 'lucide-react'
import { QRCodeSVG } from 'qrcode.react'
import { useCallback, useEffect, useRef, useState } from 'react'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'

import { Dialog } from '@/components/dialog'
import { Button } from '@/components/ui/button'
import { useCopyToClipboard } from '@/hooks/use-copy-to-clipboard'

import {
  cancelTopupPayment,
  getTopupPaymentStatus,
  isApiSuccess,
} from '../../api'
import type { EpayQRCodePaymentData } from '../../types'

interface WeChatPaymentDialogProps {
  open: boolean
  onOpenChange: (open: boolean) => void
  payment: EpayQRCodePaymentData | null
  onPaid?: () => void | Promise<void>
}

type PaymentDisplayStatus = 'pending' | 'success' | 'expired'

function formatRemainingTime(totalSeconds: number) {
  const minutes = Math.floor(totalSeconds / 60)
  const seconds = totalSeconds % 60
  return `${minutes.toString().padStart(2, '0')}:${seconds
    .toString()
    .padStart(2, '0')}`
}

export function WeChatPaymentDialog({
  open,
  onOpenChange,
  payment,
  onPaid,
}: WeChatPaymentDialogProps) {
  const { t } = useTranslation()
  const { copiedText, copyToClipboard } = useCopyToClipboard({ notify: false })
  const paymentTradeNo = payment?.trade_no
  const [paymentState, setPaymentState] = useState<{
    tradeNo: string | null
    status: PaymentDisplayStatus
  }>({ tradeNo: null, status: 'pending' })
  const [checking, setChecking] = useState(false)
  const [cancelling, setCancelling] = useState(false)
  const [nowSeconds, setNowSeconds] = useState(() =>
    Math.floor(Date.now() / 1000)
  )
  const completedRef = useRef(false)
  const status =
    paymentState.tradeNo === paymentTradeNo ? paymentState.status : 'pending'
  const remainingSeconds = Math.max(0, (payment?.expires_at ?? 0) - nowSeconds)
  const displayStatus =
    status === 'pending' && payment?.expires_at && remainingSeconds === 0
      ? 'expired'
      : status

  const checkPaymentStatus = useCallback(async () => {
    if (!paymentTradeNo || completedRef.current || cancelling) return

    setChecking(true)
    try {
      const response = await getTopupPaymentStatus(paymentTradeNo)
      if (completedRef.current) return
      if (!isApiSuccess(response) || !response.data) return

      if (response.data.status === 'success') {
        completedRef.current = true
        setPaymentState({ tradeNo: paymentTradeNo, status: 'success' })
        await onPaid?.()
        return
      }

      if (
        response.data.status === 'expired' ||
        response.data.status === 'cancelled' ||
        response.data.status === 'failed'
      ) {
        completedRef.current = true
        setPaymentState({ tradeNo: paymentTradeNo, status: 'expired' })
      }
    } catch {
      // Keep polling quietly; the user can use the manual check button when
      // they want immediate feedback after a transient network failure.
    } finally {
      setChecking(false)
    }
  }, [cancelling, onPaid, paymentTradeNo])

  useEffect(() => {
    if (!open || !paymentTradeNo) return

    completedRef.current = false
    const timer = window.setInterval(() => {
      void checkPaymentStatus()
    }, 3000)

    return () => window.clearInterval(timer)
  }, [checkPaymentStatus, open, paymentTradeNo])

  useEffect(() => {
    if (!open || !paymentTradeNo) return

    const timer = window.setInterval(() => {
      setNowSeconds(Math.floor(Date.now() / 1000))
    }, 1000)

    return () => window.clearInterval(timer)
  }, [open, paymentTradeNo])

  const handleCopyLink = async () => {
    if (payment?.qrcode) {
      await copyToClipboard(payment.qrcode)
    }
  }

  const handleManualCheck = async () => {
    await checkPaymentStatus()
  }

  const handleCancel = useCallback(async () => {
    if (!paymentTradeNo || cancelling) return
    if (status === 'success' || completedRef.current) {
      onOpenChange(false)
      return
    }

    setCancelling(true)
    try {
      // Check the gateway once more before expiring the order. This closes
      // the race where the user paid but the asynchronous callback is late.
      const latest = await getTopupPaymentStatus(paymentTradeNo)
      if (!isApiSuccess(latest) || !latest.data) {
        toast.error(t('Failed to cancel payment'))
        return
      }
      if (latest.data.status === 'success') {
        completedRef.current = true
        setPaymentState({ tradeNo: paymentTradeNo, status: 'success' })
        await onPaid?.()
        onOpenChange(false)
        return
      }

      completedRef.current = true
      const response = await cancelTopupPayment(paymentTradeNo)
      if (!isApiSuccess(response)) {
        completedRef.current = false
        toast.error(response.message || t('Failed to cancel payment'))
        return
      }

      setPaymentState({ tradeNo: paymentTradeNo, status: 'expired' })
      onOpenChange(false)
    } catch {
      completedRef.current = false
      toast.error(t('Failed to cancel payment'))
    } finally {
      setCancelling(false)
    }
  }, [cancelling, onOpenChange, onPaid, paymentTradeNo, status, t])

  const handleOpenChange = useCallback(
    (nextOpen: boolean) => {
      if (nextOpen) {
        onOpenChange(true)
        return
      }
      void handleCancel()
    },
    [handleCancel, onOpenChange]
  )

  if (!payment) return null

  let statusText = t('Waiting for payment...')
  if (displayStatus === 'success') {
    statusText = t('Payment successful')
  } else if (displayStatus === 'expired') {
    statusText = t('Payment expired')
  }

  let statusClassName =
    'text-muted-foreground flex items-center gap-2 text-sm font-medium'
  if (displayStatus === 'success') {
    statusClassName =
      'flex items-center gap-2 text-sm font-medium text-green-600'
  } else if (displayStatus === 'expired') {
    statusClassName = 'text-destructive text-sm font-medium'
  }

  return (
    <Dialog
      open={open}
      onOpenChange={handleOpenChange}
      title={
        <span className='flex items-center gap-2'>
          <span className='inline-flex h-2.5 w-2.5 rounded-full bg-[#07c160]' />
          {t('WeChat Scan to Pay')}
        </span>
      }
      description={t(
        'Scan with WeChat. Your balance updates after payment is confirmed.'
      )}
      contentClassName='max-sm:w-[calc(100vw-1.5rem)] sm:max-w-md'
      bodyClassName='flex flex-col items-center gap-4'
    >
      {payment.qrcode && displayStatus === 'pending' ? (
        <div className='rounded-[28px] bg-white p-4 shadow-sm ring-1 ring-black/10'>
          <QRCodeSVG value={payment.qrcode} size={240} includeMargin />
        </div>
      ) : (
        <div className='text-muted-foreground bg-muted flex min-h-60 w-60 items-center justify-center rounded-[28px] p-6 text-center text-sm'>
          {displayStatus === 'expired'
            ? t('Payment expired')
            : t(
                'Payment link is temporarily unavailable. Cancel this order and try again.'
              )}
        </div>
      )}

      {payment.qrcode && displayStatus === 'pending' && (
        <Button
          variant='outline'
          size='sm'
          onClick={handleCopyLink}
          className='gap-2'
        >
          {copiedText === payment.qrcode && <Check className='h-4 w-4' />}
          {copiedText !== payment.qrcode && <Copy className='h-4 w-4' />}
          {t('Copy Payment Link')}
        </Button>
      )}

      <div className='text-muted-foreground w-full text-center text-xs'>
        {t('Order Number')}:{' '}
        <span className='font-mono'>{payment.trade_no}</span>
      </div>

      <div className={statusClassName}>
        {displayStatus === 'success' && <Check className='h-4 w-4' />}
        {displayStatus !== 'success' && checking && (
          <Loader2 className='h-4 w-4 animate-spin' />
        )}
        {statusText}
      </div>

      {displayStatus === 'pending' && (
        <div className='text-muted-foreground text-center text-xs'>
          {t('Time remaining')}: {formatRemainingTime(remainingSeconds)}
        </div>
      )}

      <div className='flex w-full flex-col-reverse gap-2 sm:flex-row sm:justify-center'>
        <Button
          variant='outline'
          onClick={handleManualCheck}
          disabled={checking || displayStatus === 'success'}
          className='gap-2'
        >
          <RefreshCw className='h-4 w-4' />
          {t('Check Payment Result')}
        </Button>
        <Button
          variant='ghost'
          onClick={() => void handleCancel()}
          disabled={cancelling}
          className='gap-2'
        >
          {displayStatus === 'pending' && <Ban className='h-4 w-4' />}
          {displayStatus === 'pending'
            ? t('Cancel Payment')
            : t('Back to Wallet')}
        </Button>
      </div>
    </Dialog>
  )
}
