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
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { render, screen, waitFor, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { afterEach, describe, expect, it, vi } from 'vitest'

import { previewUsdtNodes, testUsdtNodes, updateUsdtSettings } from '../api'
import { UsdtSettingsForm } from '../components/admin/usdt-settings-form'
import { splitPastedUrls } from '../lib'
import type { UsdtChainSetting, UsdtRpcNode, UsdtSettings } from '../types'

vi.mock('../api', () => ({
  updateUsdtSettings: vi.fn(),
  verifyUsdtFactory: vi.fn(),
  testUsdtNodes: vi.fn(),
  previewUsdtNodes: vi.fn(),
}))
vi.mock('sonner', () => ({ toast: { success: vi.fn(), error: vi.fn() } }))
vi.mock('@/features/system-settings/components/settings-page-context', () => ({
  SettingsPageFormActions: (props: { onSave: () => void }) => (
    <button type='button' onClick={props.onSave}>
      Save
    </button>
  ),
}))

function node(id: string, extra: Partial<UsdtRpcNode> = {}): UsdtRpcNode {
  return {
    id,
    name: id,
    provider: '',
    provider_key: 'ankr',
    provider_name: 'Ankr',
    provider_known: true,
    provider_inferred: false,
    endpoint: 'https://rpc.ankr.com/…',
    priority: 10,
    enabled: true,
    health: { state: 'unknown', failures: 0, methods: [] },
    ...extra,
  }
}

function settings(nodes: UsdtRpcNode[]): UsdtSettings {
  const bsc: UsdtChainSetting = {
    id: 'bsc',
    name: 'BSC (BEP20)',
    contract: '0x55d3',
    enabled: false,
    factory_address: '0x11',
    factory_display: '0x11',
    implementation_address: '',
    verified: true,
    finality: 'finalized',
    confirmations: 0,
    min_amount: 1,
    sweep_threshold: 1,
    address_count: 0,
    factory_locked: false,
    nodes,
    enabled_nodes: nodes.length,
    providers: nodes.length,
    available_providers: nodes.length,
    status: 'normal',
  }
  return {
    enabled: true,
    admin_only: true,
    rate_mode: 'fixed',
    fixed_rate: 1,
    scanner_ips: '',
    has_scanner_secret: true,
    chains: [bsc],
    heartbeat: null,
    heartbeat_at: 0,
    scanner_version: '',
    alerts: null,
  }
}

/** Renders the form and unfolds the BSC card, folded by default. */
async function renderForm(value: UsdtSettings) {
  const client = new QueryClient({
    defaultOptions: { mutations: { retry: false } },
  })
  render(
    <QueryClientProvider client={client}>
      <UsdtSettingsForm settings={value} />
    </QueryClientProvider>
  )
  await userEvent.click(screen.getByRole('button', { name: /BSC \(BEP20\)/ }))
}

afterEach(() => vi.clearAllMocks())

describe('USDT network card', () => {
  it('starts folded with its status visible and unfolds on click', async () => {
    const client = new QueryClient()
    render(
      <QueryClientProvider client={client}>
        <UsdtSettingsForm
          settings={{
            ...settings([node('a'), node('b')]),
            chains: [{ ...settings([]).chains[0], status: 'congested' }],
          }}
        />
      </QueryClientProvider>
    )
    const header = screen.getByRole('button', { name: /BSC \(BEP20\)/ })
    expect(header).toHaveAttribute('aria-expanded', 'false')
    expect(header).toHaveTextContent('Congested')
    expect(screen.queryByText('Verification nodes')).not.toBeVisible()

    await userEvent.click(header)
    expect(header).toHaveAttribute('aria-expanded', 'true')
    expect(screen.getByText('Verification nodes')).toBeVisible()
  })
})

describe('splitPastedUrls', () => {
  it('splits lines and commas and drops blanks', () => {
    expect(
      splitPastedUrls(
        ' https://a.example \n\nhttps://b.example,https://c.example\n'
      )
    ).toEqual(['https://a.example', 'https://b.example', 'https://c.example'])
  })
})

describe('USDT verification node list', () => {
  it('shows endpoints and providers counts and masked endpoints only', async () => {
    await renderForm(
      settings([
        node('a'),
        node('b', {
          provider_key: 'drpc',
          provider_name: 'dRPC',
          endpoint: 'https://lb.drpc.org/…',
        }),
      ])
    )
    expect(
      screen.getByText('2 enabled endpoints / 2 independent providers')
    ).toBeInTheDocument()
    expect(screen.getByText('https://lb.drpc.org/…')).toBeInTheDocument()
    expect(screen.queryByLabelText('RPC URL')).not.toBeInTheDocument()
  })

  it('saves the reordered list without resending saved URLs', async () => {
    vi.mocked(updateUsdtSettings).mockImplementation(
      () => new Promise(() => {})
    )
    const user = userEvent.setup()
    await renderForm(settings([node('first'), node('second')]))

    await user.click(screen.getAllByRole('button', { name: 'Move down' })[0])
    await user.click(screen.getByRole('button', { name: 'Save' }))

    await waitFor(() => expect(updateUsdtSettings).toHaveBeenCalled())
    const patch = vi.mocked(updateUsdtSettings).mock.calls[0][0]
    expect(patch.chains?.bsc.rpc_nodes?.map((n) => n.id)).toEqual([
      'second',
      'first',
    ])
    expect(patch.chains?.bsc.rpc_nodes?.every((n) => n.url === '')).toBe(true)
  })

  it('warns that a new endpoint without a known operator is not counted', async () => {
    const user = userEvent.setup()
    await renderForm(settings([node('a'), node('b')]))
    await user.click(screen.getByRole('button', { name: 'Add endpoint' }))
    expect(screen.getByLabelText('RPC URL')).toBeInTheDocument()
    expect(
      screen.getByText(
        'Not counted as an independent provider until you name its operator.'
      )
    ).toBeInTheDocument()
    await user.type(screen.getAllByLabelText('Operator')[2], 'Our node')
    expect(
      screen.queryByText(
        'Not counted as an independent provider until you name its operator.'
      )
    ).not.toBeInTheDocument()
  })

  it('adds pasted URLs with the recognized or named operator', async () => {
    vi.mocked(previewUsdtNodes).mockResolvedValue([
      {
        url: 'https://rpc.ankr.com/bsc/KEY',
        endpoint: 'https://rpc.ankr.com/…',
        valid: true,
        provider_key: 'ankr',
        provider_name: 'Ankr',
        known: true,
        domain: 'ankr.com',
      },
      {
        url: 'https://bsc.my-node.example',
        endpoint: 'https://bsc.my-node.example',
        valid: true,
        provider_key: '',
        provider_name: '',
        known: false,
        domain: 'my-node.example',
      },
    ])
    vi.mocked(updateUsdtSettings).mockImplementation(
      () => new Promise(() => {})
    )
    const user = userEvent.setup()
    await renderForm(settings([]))

    await user.type(
      screen.getByLabelText('Paste RPC URLs, one per line'),
      'https://rpc.ankr.com/bsc/KEY{enter}https://bsc.my-node.example'
    )
    await user.click(
      screen.getByRole('button', { name: 'Recognize providers' })
    )
    await user.type(
      await screen.findByLabelText('Operator of my-node.example'),
      'Our node'
    )
    await user.click(screen.getByRole('button', { name: 'Add 2 endpoints' }))
    await user.click(screen.getByRole('button', { name: 'Save' }))

    await waitFor(() => expect(updateUsdtSettings).toHaveBeenCalled())
    const nodes =
      vi.mocked(updateUsdtSettings).mock.calls[0][0].chains?.bsc.rpc_nodes
    expect(nodes).toEqual([
      {
        id: '',
        name: '',
        provider: 'Ankr',
        url: 'https://rpc.ankr.com/bsc/KEY',
        enabled: true,
      },
      {
        id: '',
        name: '',
        provider: 'Our node',
        url: 'https://bsc.my-node.example',
        enabled: true,
      },
    ])
  })

  it('shows the per-method result of a node test', async () => {
    vi.mocked(testUsdtNodes).mockResolvedValue([
      {
        node_id: 'a',
        provider: 'dRPC',
        endpoint: 'https://lb.drpc.org/…',
        ok: false,
        millis: 120,
        checks: [
          { method: 'eth_chainId', ok: true, detail: '56' },
          {
            method: 'eth_getBlockByNumber(finalized)',
            ok: false,
            detail: 'HTTP 403',
          },
        ],
      },
    ])
    const user = userEvent.setup()
    await renderForm(settings([node('a'), node('b')]))
    await user.click(screen.getAllByRole('button', { name: 'Test' })[0])
    const result = await screen.findByRole('list', { name: 'Test result' })
    expect(within(result).getAllByRole('listitem')[1]).toHaveTextContent(
      'eth_getBlockByNumber(finalized): HTTP 403'
    )
    expect(testUsdtNodes).toHaveBeenCalledWith('bsc', 'a')
  })
})
