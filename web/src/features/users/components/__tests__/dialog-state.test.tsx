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

import { UsersProvider, useUsers, useUsersDialog } from '../users-provider'

afterEach(() => {
  cleanup()
})

const rowRenders = vi.fn()

// Stands in for a memoized row or cell that reads list-level state.
const RowProbe = memo(function RowProbe() {
  const state = useUsers()
  rowRenders()
  return <span>{String(state.refreshTrigger)}</span>
})

function OpenButton() {
  const state = useUsers()
  return (
    <button type='button' onClick={() => state.setOpen('delete')}>
      Open
    </button>
  )
}

function DialogProbe() {
  const dialog = useUsersDialog()
  return <output aria-label='dialog'>{dialog.open ?? 'closed'}</output>
}

test('opening a user dialog re-renders dialog readers but not rows', async () => {
  render(
    <QueryClientProvider client={new QueryClient()}>
      <UsersProvider>
        <RowProbe />
        <OpenButton />
        <DialogProbe />
      </UsersProvider>
    </QueryClientProvider>
  )
  rowRenders.mockClear()

  await userEvent.click(screen.getByRole('button', { name: 'Open' }))

  expect(screen.getByLabelText('dialog')).toHaveTextContent('delete')
  expect(rowRenders).not.toHaveBeenCalled()
})
