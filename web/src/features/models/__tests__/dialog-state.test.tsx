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
import { cleanup, render, screen } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { memo } from 'react'
import { afterEach, expect, test, vi } from 'vitest'

import {
  ModelsProvider,
  useModels,
  useModelsDialog,
} from '../components/models-provider'

afterEach(() => {
  cleanup()
})

const rowRenders = vi.fn()

// Stands in for a memoized row or cell that reads list-level state.
const RowProbe = memo(function RowProbe() {
  const state = useModels()
  rowRenders()
  return <span>{String(state.selectedVendor)}</span>
})

function OpenButton() {
  const state = useModels()
  return (
    <button type='button' onClick={() => state.setOpen('create-model')}>
      Open
    </button>
  )
}

function DialogProbe() {
  const dialog = useModelsDialog()
  return <output aria-label='dialog'>{dialog.open ?? 'closed'}</output>
}

test('opening a model dialog re-renders dialog readers but not rows', async () => {
  render(
    <ModelsProvider>
      <RowProbe />
      <OpenButton />
      <DialogProbe />
    </ModelsProvider>
  )
  rowRenders.mockClear()

  await userEvent.click(screen.getByRole('button', { name: 'Open' }))

  expect(screen.getByLabelText('dialog')).toHaveTextContent('create-model')
  expect(rowRenders).not.toHaveBeenCalled()
})
