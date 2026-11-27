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
import { ArrowDown, ArrowUp, Trash2 } from 'lucide-react'
import { type Control, useWatch } from 'react-hook-form'
import { useTranslation } from 'react-i18next'

import { PasswordInput } from '@/components/password-input'
import { StatusBadge } from '@/components/status-badge'
import { Button } from '@/components/ui/button'
import { FormControl, FormField, FormItem } from '@/components/ui/form'
import { Input } from '@/components/ui/input'
import { Switch } from '@/components/ui/switch'
import { formatTimestamp } from '@/lib/format'

import type { UsdtNodeTestResult, UsdtRpcNode } from '../../types'
import type { UsdtSettingsValues } from './usdt-settings-form'

interface UsdtRpcNodeRowProps {
  control: Control<UsdtSettingsValues>
  chainIndex: number
  index: number
  count: number
  /** the saved node, absent for a row added in this edit */
  saved?: UsdtRpcNode
  testResult?: UsdtNodeTestResult
  testing: boolean
  onMove: (to: number) => void
  onRemove: () => void
  onTest: () => void
}

function NodeHealth(props: { node: UsdtRpcNode }) {
  const { t } = useTranslation()
  const health = props.node.health
  let badge = <StatusBadge label={t('Not used yet')} variant='neutral' />
  if (health.state === 'healthy') {
    badge = <StatusBadge label={t('Healthy')} variant='success' />
  }
  if (health.state === 'cooling') {
    badge = (
      <StatusBadge
        label={t('Resting until {{time}}', {
          time: formatTimestamp(health.cooling_until ?? 0),
        })}
        variant='warning'
      />
    )
  }
  return (
    <div className='space-y-1 text-xs'>
      <div className='flex flex-wrap items-center gap-2'>
        {badge}
        {health.last_error && (
          <span className='text-destructive break-all'>
            {health.last_error}
          </span>
        )}
      </div>
      {health.methods.length > 0 && (
        <ul className='text-muted-foreground flex flex-wrap gap-x-3 font-mono'>
          {health.methods.map((m) => (
            <li key={m.method} className={m.ok ? '' : 'text-destructive'}>
              {m.method} {m.ok ? t('OK') : (m.detail ?? t('Failed'))}
            </li>
          ))}
        </ul>
      )}
    </div>
  )
}

function NodeTestChecks(props: { result: UsdtNodeTestResult }) {
  const { t } = useTranslation()
  return (
    <ul className='space-y-0.5 font-mono text-xs' aria-label={t('Test result')}>
      {props.result.checks.map((c) => (
        <li
          key={c.method}
          className={c.ok ? 'text-success' : 'text-destructive'}
        >
          {c.ok ? '✓' : '✗'} {c.method}
          {c.detail ? `: ${c.detail}` : ''}
        </li>
      ))}
    </ul>
  )
}

/** One verification endpoint: order, name, operator, state and test. */
export function UsdtRpcNodeRow(props: UsdtRpcNodeRowProps) {
  const { t } = useTranslation()
  const prefix = `chains.${props.chainIndex}.rpc_nodes.${props.index}` as const
  const provider = useWatch({
    control: props.control,
    name: `${prefix}.provider`,
  })
  const saved = props.saved
  const recognized = saved?.provider_known ? saved.provider_name : ''
  const uncounted = !recognized && !provider.trim()

  return (
    <li className='space-y-2 rounded-md border p-3'>
      <div className='flex flex-wrap items-center gap-2'>
        <span className='text-muted-foreground w-6 text-xs tabular-nums'>
          {props.index + 1}
        </span>
        <FormField
          control={props.control}
          name={`${prefix}.enabled`}
          render={({ field }) => (
            <FormItem>
              <FormControl>
                <Switch
                  checked={field.value}
                  onCheckedChange={field.onChange}
                  aria-label={t('Use this endpoint')}
                />
              </FormControl>
            </FormItem>
          )}
        />
        <FormField
          control={props.control}
          name={`${prefix}.name`}
          render={({ field }) => (
            <FormItem className='min-w-32 flex-1'>
              <FormControl>
                <Input
                  placeholder={t('Name')}
                  aria-label={t('Name')}
                  {...field}
                />
              </FormControl>
            </FormItem>
          )}
        />
        <FormField
          control={props.control}
          name={`${prefix}.provider`}
          render={({ field }) => (
            <FormItem className='min-w-32 flex-1'>
              <FormControl>
                <Input
                  placeholder={recognized || t('Operator')}
                  aria-label={t('Operator')}
                  disabled={!!recognized}
                  {...field}
                />
              </FormControl>
            </FormItem>
          )}
        />
        <div className='flex items-center gap-1'>
          <Button
            type='button'
            variant='ghost'
            size='icon-sm'
            disabled={props.index === 0}
            onClick={() => props.onMove(props.index - 1)}
            aria-label={t('Move up')}
          >
            <ArrowUp className='size-4' aria-hidden />
          </Button>
          <Button
            type='button'
            variant='ghost'
            size='icon-sm'
            disabled={props.index === props.count - 1}
            onClick={() => props.onMove(props.index + 1)}
            aria-label={t('Move down')}
          >
            <ArrowDown className='size-4' aria-hidden />
          </Button>
          <Button
            type='button'
            variant='outline'
            size='sm'
            disabled={!saved || props.testing}
            onClick={props.onTest}
          >
            {t('Test')}
          </Button>
          <Button
            type='button'
            variant='ghost'
            size='icon-sm'
            onClick={props.onRemove}
            aria-label={t('Remove endpoint')}
          >
            <Trash2 className='size-4' aria-hidden />
          </Button>
        </div>
      </div>
      {saved ? (
        <p className='text-muted-foreground font-mono text-xs break-all'>
          {saved.endpoint}
        </p>
      ) : (
        <FormField
          control={props.control}
          name={`${prefix}.url`}
          render={({ field }) => (
            <FormItem>
              <FormControl>
                <PasswordInput
                  autoComplete='off'
                  placeholder='https://'
                  aria-label={t('RPC URL')}
                  {...field}
                />
              </FormControl>
            </FormItem>
          )}
        />
      )}
      {uncounted && (
        <p className='text-warning text-xs'>
          {t(
            'Not counted as an independent provider until you name its operator.'
          )}
        </p>
      )}
      {saved?.provider_inferred && (
        <p className='text-warning text-xs'>
          {t(
            'Operator guessed from the old two-node settings. Confirm it is independent of the others.'
          )}
        </p>
      )}
      {saved && <NodeHealth node={saved} />}
      {props.testResult && <NodeTestChecks result={props.testResult} />}
    </li>
  )
}
