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

import { api } from '@/lib/api'

import { AnnouncementsSection } from '../announcements-section'

type SavedAnnouncement = { id: number; content: string; popup?: boolean }

const publishDate = '2026-11-01T10:00:00.000Z'

function renderSection(data: unknown[]) {
  const client = new QueryClient({
    defaultOptions: { mutations: { retry: false } },
  })
  render(
    <QueryClientProvider client={client}>
      <AnnouncementsSection enabled data={JSON.stringify(data)} />
    </QueryClientProvider>
  )
}

async function saveAndReadAnnouncements(
  user: ReturnType<typeof userEvent.setup>,
  put: ReturnType<typeof vi.spyOn>
): Promise<SavedAnnouncement[]> {
  await user.click(screen.getByRole('button', { name: 'Save Settings' }))
  await waitFor(() => expect(put).toHaveBeenCalled())
  const body = put.mock.calls[0][1] as { key: string; value: string }
  expect(body.key).toBe('console_setting.announcements')
  return JSON.parse(body.value)
}

describe('AnnouncementsSection sign-in popup', () => {
  afterEach(() => {
    vi.restoreAllMocks()
  })

  it('saves a new popup announcement with an ID no deleted announcement used', async () => {
    const put = vi
      .spyOn(api, 'put')
      .mockResolvedValue({ data: { success: true } })
    const user = userEvent.setup()
    renderSection([{ id: 5, content: 'Old', publishDate, type: 'default' }])

    await user.click(screen.getByRole('button', { name: 'Add Announcement' }))
    const dialog = await screen.findByRole('dialog')
    await user.type(
      within(dialog).getByRole('textbox', { name: 'Content' }),
      'Read me first'
    )
    await user.click(
      within(dialog).getByRole('switch', { name: 'Pop up after sign-in' })
    )
    await user.click(within(dialog).getByRole('button', { name: 'Add' }))
    const saved = await saveAndReadAnnouncements(user, put)

    const added = saved.find((item) => item.content === 'Read me first')
    expect(added?.popup).toBe(true)
    expect(added?.id).toBeGreaterThanOrEqual(Date.now() - 60_000)
  })

  it('gives legacy entries without IDs or sharing one a distinct ID on save', async () => {
    const put = vi
      .spyOn(api, 'put')
      .mockResolvedValue({ data: { success: true } })
    const user = userEvent.setup()
    renderSection([
      { id: 2, content: 'A', publishDate, type: 'default' },
      { content: 'B', publishDate, type: 'default' },
      { id: 2, content: 'C', publishDate, type: 'default' },
    ])

    // Saving is enabled only after a change, so add an entry first
    await user.click(screen.getByRole('button', { name: 'Add Announcement' }))
    const dialog = await screen.findByRole('dialog')
    await user.type(
      within(dialog).getByRole('textbox', { name: 'Content' }),
      'D'
    )
    await user.click(within(dialog).getByRole('button', { name: 'Add' }))
    const saved = await saveAndReadAnnouncements(user, put)

    const ids = saved.map((item) => item.id)
    expect(new Set(ids).size).toBe(ids.length)
    expect(saved.find((item) => item.content === 'A')?.id).toBe(2)
  })
})
