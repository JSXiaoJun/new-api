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
import { ChevronLeft, ChevronRight, RefreshCw } from 'lucide-react'
import { useState } from 'react'
import { useTranslation } from 'react-i18next'

import { Dialog } from '@/components/dialog'
import { Alert, AlertTitle } from '@/components/ui/alert'
import { Button } from '@/components/ui/button'
import { NativeSelect, NativeSelectOption } from '@/components/ui/native-select'
import { Skeleton } from '@/components/ui/skeleton'
import { Tabs, TabsContent, TabsList, TabsTrigger } from '@/components/ui/tabs'
import {
  Tooltip,
  TooltipContent,
  TooltipTrigger,
} from '@/components/ui/tooltip'

import { getUserQuotaLogs } from '../../api'
import type {
  UserQuotaCreditsPage,
  UserQuotaHistoryView,
  UserQuotaLogsPage,
} from '../../types'
import {
  UserQuotaCreditsTable,
  UserQuotaLegacyTable,
} from './user-quota-history-tables'

interface UserQuotaHistoryDialogProps {
  open: boolean
  onOpenChange: (open: boolean) => void
  user: { id: number; username: string }
}

export function UserQuotaHistoryDialog(props: UserQuotaHistoryDialogProps) {
  const { t } = useTranslation()
  const [view, setView] = useState<UserQuotaHistoryView>('credits')
  const [pagination, setPagination] = useState({ page: 1, pageSize: 10 })
  const query = useQuery<
    | { view: 'credits'; page: UserQuotaCreditsPage }
    | { view: 'legacy'; page: UserQuotaLogsPage }
  >({
    queryKey: [
      'user-quota-logs',
      props.user.id,
      view,
      pagination.page,
      pagination.pageSize,
    ],
    enabled: props.open,
    placeholderData: (previousData, previousQuery) =>
      previousQuery?.queryKey[1] === props.user.id &&
      previousQuery?.queryKey[2] === view
        ? previousData
        : undefined,
    queryFn: async ({ signal }) => {
      if (view === 'legacy') {
        const result = await getUserQuotaLogs(
          props.user.id,
          {
            p: pagination.page,
            page_size: pagination.pageSize,
            view: 'legacy',
          },
          signal
        )
        if (!result.success || !result.data) {
          throw new Error(result.message || t('Failed to load quota records'))
        }
        return { view: 'legacy' as const, page: result.data }
      }
      const result = await getUserQuotaLogs(
        props.user.id,
        { p: pagination.page, page_size: pagination.pageSize, view: 'credits' },
        signal
      )
      if (!result.success || !result.data) {
        throw new Error(result.message || t('Failed to load quota records'))
      }
      return { view: 'credits' as const, page: result.data }
    },
  })
  const total = query.data?.page.total ?? 0
  const totalPages = Math.max(1, Math.ceil(total / pagination.pageSize))

  return (
    <Dialog
      open={props.open}
      onOpenChange={props.onOpenChange}
      title={t('Quota History')}
      description={`${props.user.username} (${t('User ID')}: ${props.user.id})`}
      headerClassName='pr-6'
      descriptionClassName='break-all'
      contentClassName='sm:max-w-4xl'
      bodyClassName='min-h-48'
      footer={
        <div className='flex w-full flex-wrap items-center justify-between gap-3'>
          <div className='flex flex-wrap items-center gap-2 text-sm'>
            <span className='text-muted-foreground'>
              {t('Total:')} {total.toLocaleString()}
            </span>
            <NativeSelect
              aria-label={t('Rows per page')}
              value={pagination.pageSize}
              disabled={query.isFetching}
              onChange={(event) =>
                setPagination({ page: 1, pageSize: Number(event.target.value) })
              }
            >
              {[10, 20, 50, 100].map((size) => (
                <NativeSelectOption key={size} value={size}>
                  {size}
                </NativeSelectOption>
              ))}
            </NativeSelect>
            <Tooltip>
              <TooltipTrigger
                render={
                  <Button
                    variant='ghost'
                    size='icon-sm'
                    aria-label={t('Refresh')}
                    disabled={query.isFetching}
                    onClick={() => void query.refetch()}
                  />
                }
              >
                <RefreshCw />
              </TooltipTrigger>
              <TooltipContent>{t('Refresh')}</TooltipContent>
            </Tooltip>
          </div>
          <div className='flex items-center gap-2'>
            <Button
              variant='outline'
              size='icon-sm'
              aria-label={t('Go to previous page')}
              disabled={pagination.page <= 1 || query.isFetching}
              onClick={() =>
                setPagination((current) => ({
                  ...current,
                  page: current.page - 1,
                }))
              }
            >
              <ChevronLeft />
            </Button>
            <span
              className='text-muted-foreground text-sm tabular-nums'
              aria-live='polite'
            >
              {t('Page {{current}} of {{total}}', {
                current: pagination.page,
                total: totalPages,
              })}
            </span>
            <Button
              variant='outline'
              size='icon-sm'
              aria-label={t('Go to next page')}
              disabled={
                pagination.page >= totalPages ||
                query.isFetching ||
                query.isError
              }
              onClick={() =>
                setPagination((current) => ({
                  ...current,
                  page: current.page + 1,
                }))
              }
            >
              <ChevronRight />
            </Button>
          </div>
        </div>
      }
    >
      <Tabs
        value={view}
        onValueChange={(value) => {
          if (value !== 'credits' && value !== 'legacy') return
          setView(value)
          setPagination((current) => ({ ...current, page: 1 }))
        }}
        className='min-w-0 gap-4'
      >
        <TabsList aria-label={t('Quota History')}>
          <TabsTrigger value='credits'>{t('Credit records')}</TabsTrigger>
          <TabsTrigger value='legacy'>{t('Historical logs')}</TabsTrigger>
        </TabsList>
        <p className='text-muted-foreground text-xs'>
          {t(
            'Wallet credit records begin after auditing is enabled. Unrecorded or deleted history cannot be recovered.'
          )}
        </p>
        {query.isError ? (
          <Alert variant='destructive'>
            <AlertTitle>{t('Failed to load quota records')}</AlertTitle>
            <Button
              variant='outline'
              size='sm'
              disabled={query.isFetching}
              onClick={() => void query.refetch()}
            >
              <RefreshCw />
              {t('Retry')}
            </Button>
          </Alert>
        ) : null}
        {query.isPending || query.isPlaceholderData ? (
          <div role='status' aria-label={t('Loading...')} className='space-y-3'>
            {[0, 1, 2, 3, 4].map((row) => (
              <Skeleton key={row} className='h-10 w-full' />
            ))}
          </div>
        ) : null}
        <TabsContent value='credits' className='min-w-0'>
          {!query.isPlaceholderData &&
          !query.isError &&
          query.data?.view === 'credits' ? (
            <UserQuotaCreditsTable
              records={query.data.page.items}
              busy={query.isFetching}
            />
          ) : null}
        </TabsContent>
        <TabsContent value='legacy' className='min-w-0 space-y-3'>
          <p className='text-muted-foreground text-xs'>
            {t(
              'Historical logs may not identify the amount or balance account. They do not prove wallet credit.'
            )}
          </p>
          {!query.isPlaceholderData &&
          !query.isError &&
          query.data?.view === 'legacy' ? (
            <UserQuotaLegacyTable
              records={query.data.page.items}
              busy={query.isFetching}
            />
          ) : null}
        </TabsContent>
      </Tabs>
    </Dialog>
  )
}
