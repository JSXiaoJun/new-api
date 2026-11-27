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
import { Plus } from 'lucide-react'
import { useState } from 'react'
import { type Control, useFieldArray, useFormState } from 'react-hook-form'
import { useTranslation } from 'react-i18next'

import { StatusBadge } from '@/components/status-badge'
import { Button } from '@/components/ui/button'

import { testUsdtNodes } from '../../api'
import { USDT_QUERY_KEYS } from '../../lib'
import type { UsdtChainSetting, UsdtNodeTestResult } from '../../types'
import { UsdtRpcNodePaste } from './usdt-rpc-node-paste'
import { UsdtRpcNodeRow } from './usdt-rpc-node-row'
import type { UsdtSettingsValues } from './usdt-settings-form'

/** Rows shown per page; the list itself has no size limit. */
export const USDT_NODES_PAGE_SIZE = 20

/**
 * A network's verification endpoints. A deposit is credited once two
 * independent providers from this list agree; the order is the priority.
 */
export function UsdtRpcNodeList(props: {
  chain: UsdtChainSetting
  chainIndex: number
  control: Control<UsdtSettingsValues>
}) {
  const { t } = useTranslation()
  const queryClient = useQueryClient()
  const nodes = useFieldArray({
    control: props.control,
    name: `chains.${props.chainIndex}.rpc_nodes`,
    keyName: 'key',
  })
  const { isDirty } = useFormState({ control: props.control })
  const [page, setPage] = useState(0)
  const [results, setResults] = useState<Record<string, UsdtNodeTestResult>>({})
  const test = useMutation({
    mutationFn: (nodeId?: string) => testUsdtNodes(props.chain.id, nodeId),
    onSuccess: (list) => {
      setResults((prev) => {
        const next = { ...prev }
        for (const r of list) next[r.node_id] = r
        return next
      })
      void queryClient.invalidateQueries({ queryKey: USDT_QUERY_KEYS.settings })
    },
  })
  const savedById = new Map(props.chain.nodes.map((n) => [n.id, n]))
  const lastPage = Math.max(
    0,
    Math.ceil(nodes.fields.length / USDT_NODES_PAGE_SIZE) - 1
  )
  const currentPage = Math.min(page, lastPage)
  const start = currentPage * USDT_NODES_PAGE_SIZE
  const visible = nodes.fields.slice(start, start + USDT_NODES_PAGE_SIZE)
  const enough = props.chain.providers >= 2

  return (
    <div className='space-y-3 md:col-span-2'>
      <div className='flex flex-wrap items-center gap-2'>
        <h4 className='text-sm font-medium'>{t('Verification nodes')}</h4>
        <StatusBadge
          label={t(
            '{{nodes}} enabled endpoints / {{providers}} independent providers',
            {
              nodes: props.chain.enabled_nodes,
              providers: props.chain.providers,
            }
          )}
          variant={enough ? 'success' : 'warning'}
        />
        <span className='text-muted-foreground text-xs'>
          {t('{{count}} providers available now', {
            count: props.chain.available_providers,
          })}
        </span>
      </div>
      <p className='text-muted-foreground text-xs'>
        {t(
          'A deposit is credited once two independent providers report exactly the same transfer. When one fails, the next healthy provider is asked. Several URLs of one company count as one provider. Use at least three providers in production.'
        )}
      </p>
      {nodes.fields.length === 0 && (
        <p className='text-muted-foreground text-sm'>
          {t('No verification nodes yet')}
        </p>
      )}
      <ul className='space-y-2'>
        {visible.map((field, offset) => {
          const index = start + offset
          const saved = field.id ? savedById.get(field.id) : undefined
          return (
            <UsdtRpcNodeRow
              key={field.key}
              control={props.control}
              chainIndex={props.chainIndex}
              index={index}
              count={nodes.fields.length}
              saved={saved}
              testResult={saved ? results[saved.id] : undefined}
              testing={test.isPending || isDirty}
              onMove={(to) => {
                nodes.move(index, to)
                setPage(Math.floor(to / USDT_NODES_PAGE_SIZE))
              }}
              onRemove={() => nodes.remove(index)}
              onTest={() => test.mutate(saved?.id)}
            />
          )
        })}
      </ul>
      {lastPage > 0 && (
        <div className='flex items-center justify-end gap-2'>
          <span className='text-muted-foreground text-xs'>
            {t('Page {{page}} of {{total}}', {
              page: currentPage + 1,
              total: lastPage + 1,
            })}
          </span>
          <Button
            type='button'
            size='sm'
            variant='outline'
            disabled={currentPage === 0}
            onClick={() => setPage(currentPage - 1)}
          >
            {t('Previous')}
          </Button>
          <Button
            type='button'
            size='sm'
            variant='outline'
            disabled={currentPage === lastPage}
            onClick={() => setPage(currentPage + 1)}
          >
            {t('Next')}
          </Button>
        </div>
      )}
      <div className='flex flex-wrap items-center gap-2'>
        <Button
          type='button'
          variant='outline'
          size='sm'
          onClick={() => {
            nodes.append({
              id: '',
              name: '',
              provider: '',
              url: '',
              enabled: true,
            })
            setPage(Math.floor(nodes.fields.length / USDT_NODES_PAGE_SIZE))
          }}
        >
          <Plus className='size-4' aria-hidden />
          {t('Add endpoint')}
        </Button>
        <Button
          type='button'
          variant='outline'
          size='sm'
          disabled={test.isPending || isDirty || props.chain.nodes.length === 0}
          onClick={() => test.mutate(undefined)}
        >
          {t('Test all endpoints')}
        </Button>
        <span className='text-muted-foreground text-xs'>
          {isDirty
            ? t('Save your changes before testing.')
            : t(
                'Tests run from this server: chain id, finality, a final block and a receipt in it.'
              )}
        </span>
      </div>
      <UsdtRpcNodePaste
        onAdd={(added) => {
          nodes.append(added)
          setPage(
            Math.floor(
              (nodes.fields.length + added.length - 1) / USDT_NODES_PAGE_SIZE
            )
          )
        }}
      />
    </div>
  )
}
