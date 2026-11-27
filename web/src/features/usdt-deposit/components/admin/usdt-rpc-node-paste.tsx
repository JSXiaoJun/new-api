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
import { useMutation } from '@tanstack/react-query'
import { useState } from 'react'
import { useTranslation } from 'react-i18next'

import { StatusBadge } from '@/components/status-badge'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { Textarea } from '@/components/ui/textarea'

import { previewUsdtNodes } from '../../api'
import { splitPastedUrls } from '../../lib'
import type { UsdtNodePreview } from '../../types'
import type { UsdtNodeValues } from './usdt-settings-form'

/**
 * Pastes many endpoint URLs at once. The server recognizes each provider; the
 * administrator names the operator of hosts it does not know before adding.
 */
export function UsdtRpcNodePaste(props: {
  onAdd: (nodes: UsdtNodeValues[]) => void
}) {
  const { t } = useTranslation()
  const [text, setText] = useState('')
  const [previews, setPreviews] = useState<UsdtNodePreview[]>([])
  const [operators, setOperators] = useState<Record<string, string>>({})
  const preview = useMutation({
    // Duplicates collapse here; the server refuses repeated URLs on save.
    mutationFn: () => previewUsdtNodes([...new Set(splitPastedUrls(text))]),
    onSuccess: (result) => {
      setPreviews(result)
      setOperators({})
    },
  })
  const valid = previews.filter((p) => p.valid)

  const add = () => {
    props.onAdd(
      valid.map((p) => ({
        id: '',
        name: '',
        provider: p.known ? p.provider_name : (operators[p.domain] ?? ''),
        url: p.url,
        enabled: true,
      }))
    )
    setText('')
    setPreviews([])
    setOperators({})
  }

  return (
    <div className='space-y-2 rounded-md border border-dashed p-3'>
      <Textarea
        value={text}
        onChange={(event) => setText(event.target.value)}
        placeholder={t('Paste RPC URLs, one per line')}
        aria-label={t('Paste RPC URLs, one per line')}
        autoComplete='off'
        spellCheck={false}
        className='font-mono text-xs'
      />
      <Button
        type='button'
        variant='outline'
        size='sm'
        disabled={splitPastedUrls(text).length === 0 || preview.isPending}
        onClick={() => preview.mutate()}
      >
        {t('Recognize providers')}
      </Button>
      {previews.length > 0 && (
        <ul className='space-y-1 text-xs'>
          {previews.map((p) => (
            <li key={p.url} className='flex flex-wrap items-center gap-2'>
              <span className='font-mono'>{p.endpoint}</span>
              {!p.valid && (
                <StatusBadge label={t('Invalid URL')} variant='danger' />
              )}
              {p.valid && p.known && (
                <StatusBadge label={p.provider_name} variant='success' />
              )}
              {p.valid && !p.known && (
                <Input
                  className='h-7 max-w-56 text-xs'
                  value={operators[p.domain] ?? ''}
                  onChange={(event) =>
                    setOperators((prev) => ({
                      ...prev,
                      [p.domain]: event.target.value,
                    }))
                  }
                  placeholder={t('Operator of {{domain}}', {
                    domain: p.domain,
                  })}
                  aria-label={t('Operator of {{domain}}', {
                    domain: p.domain,
                  })}
                />
              )}
            </li>
          ))}
        </ul>
      )}
      {valid.length > 0 && (
        <Button type='button' size='sm' onClick={add}>
          {t('Add {{count}} endpoints', { count: valid.length })}
        </Button>
      )}
    </div>
  )
}
