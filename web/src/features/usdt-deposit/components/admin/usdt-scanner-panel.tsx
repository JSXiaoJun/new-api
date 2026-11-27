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
import { useMutation, useQueryClient } from '@tanstack/react-query'
import { useState } from 'react'
import { useTranslation } from 'react-i18next'

import { ConfirmDialog } from '@/components/confirm-dialog'
import { CopyButton } from '@/components/copy-button'
import { StatusBadge } from '@/components/status-badge'
import { Button } from '@/components/ui/button'
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from '@/components/ui/table'
import { formatTimestamp } from '@/lib/format'

import { regenerateUsdtScannerSecret } from '../../api'
import { USDT_QUERY_KEYS } from '../../lib'
import type { UsdtSettings } from '../../types'
import { UsdtCollapsibleCard } from './usdt-collapsible-card'

// The scanner reports every minute; three missed reports means it is down.
const STALE_AFTER_SECONDS = 180

export function UsdtScannerPanel(props: { settings: UsdtSettings }) {
  const { t } = useTranslation()
  const queryClient = useQueryClient()
  const [secret, setSecret] = useState<string | null>(null)
  const [confirmOpen, setConfirmOpen] = useState(false)
  const regenerate = useMutation({
    mutationFn: regenerateUsdtScannerSecret,
    onSuccess: (data) => {
      setSecret(data.secret)
      setConfirmOpen(false)
      void queryClient.invalidateQueries({ queryKey: USDT_QUERY_KEYS.settings })
    },
  })
  const seenAt = props.settings.heartbeat_at
  const online = seenAt > 0 && Date.now() / 1000 - seenAt < STALE_AFTER_SECONDS
  const heartbeat = props.settings.heartbeat ?? {}
  const failing = Object.values(heartbeat).filter((hb) => hb.last_error).length

  return (
    <UsdtCollapsibleCard
      title={t('Scanner service')}
      summary={
        <>
          <StatusBadge
            label={online ? t('Online') : t('Offline')}
            variant={online ? 'success' : 'danger'}
          />
          {failing > 0 && (
            <StatusBadge
              label={t('{{count}} networks report errors', { count: failing })}
              variant='warning'
            />
          )}
          <span className='text-muted-foreground text-xs font-normal'>
            {seenAt > 0
              ? t('Last report {{time}}, version {{version}}', {
                  time: formatTimestamp(seenAt),
                  version: props.settings.scanner_version || '-',
                })
              : t('No report received yet')}
          </span>
        </>
      }
    >
      {Object.keys(heartbeat).length > 0 && (
        <Table>
          <TableHeader>
            <TableRow>
              <TableHead>{t('Network')}</TableHead>
              <TableHead>{t('Latest block')}</TableHead>
              <TableHead>{t('Scanned to')}</TableHead>
              <TableHead>{t('Gas wallet balance')}</TableHead>
              <TableHead>{t('Pending sweeps')}</TableHead>
              <TableHead>{t('Last error')}</TableHead>
            </TableRow>
          </TableHeader>
          <TableBody>
            {Object.entries(heartbeat).map(([chain, hb]) => (
              <TableRow key={chain}>
                <TableCell>{chain.toUpperCase()}</TableCell>
                <TableCell>{hb.head}</TableCell>
                <TableCell>{hb.cursor}</TableCell>
                <TableCell>{hb.gas_balance || '-'}</TableCell>
                <TableCell>{hb.pending_sweeps}</TableCell>
                <TableCell
                  className='text-destructive max-w-64 truncate text-xs'
                  title={hb.last_error}
                >
                  {hb.last_error || '-'}
                </TableCell>
              </TableRow>
            ))}
          </TableBody>
        </Table>
      )}

      <div className='flex flex-wrap items-center gap-3'>
        <Button
          type='button'
          variant='outline'
          size='sm'
          onClick={() => setConfirmOpen(true)}
        >
          {props.settings.has_scanner_secret
            ? t('Regenerate scanner secret')
            : t('Generate scanner secret')}
        </Button>
        <span className='text-muted-foreground text-xs'>
          {t(
            'Put the secret in the scanner .env as SCANNER_SECRET. It is shown only once.'
          )}
        </span>
      </div>
      {secret && (
        <div className='bg-muted/50 flex items-center gap-2 rounded-lg border px-3 py-2'>
          <code className='min-w-0 flex-1 font-mono text-xs break-all'>
            {secret}
          </code>
          <CopyButton value={secret} aria-label={t('Copy scanner secret')} />
        </div>
      )}

      <ConfirmDialog
        open={confirmOpen}
        onOpenChange={setConfirmOpen}
        title={t('Generate a new scanner secret?')}
        desc={t(
          'The running scanner stops being accepted until you update its SCANNER_SECRET and restart it.'
        )}
        confirmText={t('Generate')}
        handleConfirm={() => regenerate.mutate()}
        isLoading={regenerate.isPending}
      />
    </UsdtCollapsibleCard>
  )
}
