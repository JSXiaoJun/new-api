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
import { cleanup, render, screen } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { memo } from 'react'
import { afterEach, expect, test, vi } from 'vitest'

import { channelSchema } from '../../types'
import {
  ChannelsProvider,
  useChannels,
  useChannelsDialog,
} from '../channels-provider'

afterEach(() => {
  cleanup()
})

const channel = channelSchema.parse({
  id: 7,
  type: 1,
  key: '',
  name: 'Row channel',
  status: 1,
  created_time: 1,
  test_time: 0,
  response_time: 0,
  balance_updated_time: 0,
})

const rowRenders = vi.fn()

// Stands in for a memoized card or cell that reads row-level state.
const RowProbe = memo(function RowProbe() {
  const channels = useChannels()
  rowRenders()
  return <span>{channels.sensitiveVisible ? 'visible' : 'hidden'}</span>
})

function EditButton() {
  const channels = useChannels()
  return (
    <button
      type='button'
      onClick={() => {
        channels.setCurrentRow(channel)
        channels.setOpen('update-channel')
      }}
    >
      Edit
    </button>
  )
}

function DialogProbe() {
  const dialog = useChannelsDialog()
  return (
    <output aria-label='dialog'>
      {dialog.open ?? 'closed'}:{dialog.currentRow?.name ?? 'none'}
    </output>
  )
}

test('opening a channel dialog re-renders dialog readers but not rows', async () => {
  render(
    <QueryClientProvider client={new QueryClient()}>
      <ChannelsProvider>
        <RowProbe />
        <EditButton />
        <DialogProbe />
      </ChannelsProvider>
    </QueryClientProvider>
  )
  rowRenders.mockClear()

  await userEvent.click(screen.getByRole('button', { name: 'Edit' }))

  expect(screen.getByLabelText('dialog')).toHaveTextContent(
    'update-channel:Row channel'
  )
  expect(rowRenders).not.toHaveBeenCalled()
})
