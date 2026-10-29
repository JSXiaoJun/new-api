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
import { useRef, useState } from 'react'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'

import { Dialog } from '@/components/dialog'
import { Button } from '@/components/ui/button'
import { CompactDateTimeRangePicker } from '@/features/usage-logs/components/compact-date-time-range-picker'
import { downloadCsv, toCsvLine } from '@/lib/csv'
import dayjs from '@/lib/dayjs'
import { handleServerError } from '@/lib/handle-server-error'

import { exportTopUpOrders } from '../api'
import { getTopUpOrderCsvHeader, getTopUpOrderCsvRow } from '../lib/export'

type ExportTopUpOrdersDialogProps = {
  /** Order number search of the page. */
  keyword: string
  /** User ID filter of the page; undefined when not filtering by user. */
  userId?: number
  /** False while the page's filters are invalid. */
  disabled?: boolean
}

/**
 * Export button for the recharge order page. It asks for the creation time
 * range, then downloads the matching orders with the page's order number and
 * user ID filters as a CSV file.
 */
export function ExportTopUpOrdersDialog(props: ExportTopUpOrdersDialogProps) {
  const { t } = useTranslation()
  const [open, setOpen] = useState(false)
  const [range, setRange] = useState<{ start?: Date; end?: Date }>({})
  const [exportedCount, setExportedCount] = useState<number | null>(null)
  const abortRef = useRef<AbortController | null>(null)

  const startMs = range.start?.getTime()
  const endMs = range.end?.getTime()
  const rangeError =
    startMs !== undefined && endMs !== undefined && endMs < startMs
      ? t('End time must be after start time')
      : null
  const exporting = exportedCount !== null
  const canExport =
    startMs !== undefined && endMs !== undefined && !rangeError && !exporting

  const handleOpenChange = (nextOpen: boolean) => {
    if (nextOpen) {
      const now = dayjs()
      setRange({
        start: now.subtract(29, 'day').startOf('day').toDate(),
        end: now.endOf('day').toDate(),
      })
    } else {
      abortRef.current?.abort()
    }
    setOpen(nextOpen)
  }

  const handleExport = async () => {
    if (!range.start || !range.end) return
    const controller = new AbortController()
    abortRef.current = controller
    const lines = [toCsvLine(getTopUpOrderCsvHeader(t))]
    setExportedCount(0)
    try {
      const count = await exportTopUpOrders(
        {
          keyword: props.keyword,
          user_id: props.userId,
          start_timestamp: Math.floor(range.start.getTime() / 1000),
          end_timestamp: Math.floor(range.end.getTime() / 1000),
        },
        {
          signal: controller.signal,
          onOrder: (order) => {
            lines.push(toCsvLine(getTopUpOrderCsvRow(order, t)))
            if (lines.length % 500 === 0) setExportedCount(lines.length - 1)
          },
        }
      )
      downloadCsv(
        `recharge-orders-${dayjs(range.start).format('YYYYMMDD')}-${dayjs(range.end).format('YYYYMMDD')}.csv`,
        lines
      )
      toast.success(t('Exported {{count}} orders', { count }))
      setOpen(false)
    } catch (error) {
      handleServerError(error, t('Failed to export recharge orders'))
    } finally {
      abortRef.current = null
      setExportedCount(null)
    }
  }

  return (
    <Dialog
      open={open}
      onOpenChange={handleOpenChange}
      trigger={
        <Button type='button' variant='outline' disabled={props.disabled}>
          {t('Export')}
        </Button>
      }
      title={t('Export recharge orders')}
      description={t(
        'Orders created in this time range are exported. The order number and user ID filters on this page also apply.'
      )}
      contentClassName='sm:max-w-md'
      footer={
        <>
          <Button
            type='button'
            variant='outline'
            onClick={() => handleOpenChange(false)}
          >
            {t('Cancel')}
          </Button>
          <Button
            type='button'
            onClick={handleExport}
            disabled={!canExport}
            aria-busy={exporting}
          >
            {exporting && <Loader2 className='animate-spin' />}
            {t('Export')}
          </Button>
        </>
      }
    >
      <div className='space-y-2'>
        <CompactDateTimeRangePicker
          start={range.start}
          end={range.end}
          onChange={setRange}
        />
        {rangeError && (
          <p role='alert' className='text-destructive text-sm'>
            {rangeError}
          </p>
        )}
        {exporting && (
          <p aria-live='polite' className='text-muted-foreground text-sm'>
            {t('Exported {{count}} orders', { count: exportedCount })}
          </p>
        )}
      </div>
    </Dialog>
  )
}
