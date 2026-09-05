import { render } from '@testing-library/react'
import { describe, expect, test, vi } from 'vitest'

import { ChannelSelectorDialog } from '../channel-selector-dialog'

function renderDialog(channels: Array<{ id: number; name: string }>) {
  return (
    <ChannelSelectorDialog
      open
      onOpenChange={vi.fn()}
      channels={channels.map((channel) => ({
        ...channel,
        base_url: 'https://upstream.example',
        status: 1,
        type: 1,
      }))}
      selectedChannelIds={[]}
      onSelectedChannelIdsChange={vi.fn()}
      channelEndpoints={{}}
      onChannelEndpointsChange={vi.fn()}
      onConfirm={vi.fn()}
    />
  )
}

describe('channel selector dialog', () => {
  test('handles channels arriving after the dialog starts loading', () => {
    const view = render(renderDialog([]))

    view.rerender(
      renderDialog([
        { id: 1, name: 'channel-1' },
        { id: 2, name: 'channel-2' },
      ])
    )

    expect(document.body.textContent).toContain('channel-1')
  })
})
