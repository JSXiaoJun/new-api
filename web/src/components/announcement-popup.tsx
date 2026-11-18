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
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { Loader2, Megaphone } from 'lucide-react'
import { useState } from 'react'
import { useTranslation } from 'react-i18next'

import { Dialog } from '@/components/dialog'
import { RichContent } from '@/components/rich-content'
import { Button } from '@/components/ui/button'
import {
  ackPopupAnnouncement,
  getPopupAnnouncements,
  type PopupAnnouncement,
} from '@/lib/api'
import { handleServerError } from '@/lib/handle-server-error'
import { formatDateTimeObject } from '@/lib/time'
import { useNotificationStore } from '@/stores/notification-store'

const POPUP_ANNOUNCEMENTS_QUERY_KEY = ['announcements', 'popup']

/**
 * Shows the published sign-in popup announcements the user has not confirmed,
 * one at a time. The dialog cannot be dismissed; only the confirm button
 * closes it, and the confirmation is stored on the account.
 */
export function AnnouncementPopup() {
  const { t } = useTranslation()
  const queryClient = useQueryClient()
  const markAnnouncementsRead = useNotificationStore(
    (s) => s.markAnnouncementsRead
  )
  // Announcements whose confirmation failed to save are hidden for this
  // session so a server error cannot lock the console
  const [skippedIds, setSkippedIds] = useState<number[]>([])

  const { data } = useQuery({
    queryKey: POPUP_ANNOUNCEMENTS_QUERY_KEY,
    queryFn: getPopupAnnouncements,
    staleTime: 1000 * 60 * 10,
    meta: { errorToast: false },
  })

  const pending = (data ?? []).filter((item) => !skippedIds.includes(item.id))
  const current = pending[0]

  const ackMutation = useMutation({
    mutationFn: (item: PopupAnnouncement) => ackPopupAnnouncement(item.id),
    onSuccess: (_, item) => {
      queryClient.setQueryData<PopupAnnouncement[]>(
        POPUP_ANNOUNCEMENTS_QUERY_KEY,
        (previous) => previous?.filter((entry) => entry.id !== item.id)
      )
      markAnnouncementsRead([`id:${item.id}`])
    },
    onError: (error, item) => {
      handleServerError(error, t('Failed to confirm the announcement'))
      setSkippedIds((previous) => [...previous, item.id])
    },
  })

  if (!current) return null

  const publishedAt = current.publishDate
    ? formatDateTimeObject(new Date(current.publishDate))
    : ''

  return (
    <Dialog
      open
      // Only the confirm button closes it: outside clicks and Escape are ignored
      onOpenChange={() => {}}
      showCloseButton={false}
      title={
        <span className='flex items-center gap-2'>
          <Megaphone className='text-primary size-5' aria-hidden='true' />
          {t('Announcement')}
        </span>
      }
      description={
        [
          publishedAt,
          pending.length > 1
            ? t('{{count}} more to read', { count: pending.length - 1 })
            : '',
        ]
          .filter(Boolean)
          .join(' · ') || undefined
      }
      contentClassName='sm:max-w-lg'
      bodyClassName='space-y-3'
      footer={
        <Button
          onClick={() => ackMutation.mutate(current)}
          disabled={ackMutation.isPending}
        >
          {ackMutation.isPending && (
            <Loader2 className='mr-2 size-4 animate-spin' aria-hidden='true' />
          )}
          {t('I have read it')}
        </Button>
      }
    >
      <div className='max-h-[min(55vh,480px)] overflow-y-auto'>
        <RichContent breaks content={current.content} />
        {current.extra ? (
          <RichContent
            breaks
            content={current.extra}
            className='text-muted-foreground mt-3 text-sm'
          />
        ) : null}
      </div>
    </Dialog>
  )
}
