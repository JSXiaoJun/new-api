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
import { render, screen, waitFor } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { afterEach, describe, expect, it, vi } from 'vitest'

import { ackPopupAnnouncement, getPopupAnnouncements } from '@/lib/api'
import { handleServerError } from '@/lib/handle-server-error'
import { useNotificationStore } from '@/stores/notification-store'

import { AnnouncementPopup } from '../announcement-popup'

vi.mock('@/lib/api', () => ({
  getPopupAnnouncements: vi.fn(),
  ackPopupAnnouncement: vi.fn(),
}))

vi.mock('@/lib/handle-server-error', () => ({
  handleServerError: vi.fn(),
}))

const first = {
  id: 1_700_000_000_001,
  content: 'Maintenance tonight',
  publishDate: '2026-11-01T10:00:00Z',
}
const second = {
  id: 1_700_000_000_002,
  content: 'New models available',
  publishDate: '2026-11-02T10:00:00Z',
}

function renderPopup() {
  const client = new QueryClient({
    defaultOptions: { queries: { retry: false }, mutations: { retry: false } },
  })
  return render(
    <QueryClientProvider client={client}>
      <AnnouncementPopup />
    </QueryClientProvider>
  )
}

describe('AnnouncementPopup', () => {
  afterEach(() => {
    vi.clearAllMocks()
    useNotificationStore.setState(useNotificationStore.getInitialState(), true)
    window.localStorage.clear()
  })

  it('renders nothing when every popup announcement is confirmed', async () => {
    vi.mocked(getPopupAnnouncements).mockResolvedValue([])

    renderPopup()

    await waitFor(() => expect(getPopupAnnouncements).toHaveBeenCalled())
    expect(screen.queryByRole('dialog')).not.toBeInTheDocument()
  })

  it('stays open on Escape and offers no close button besides confirming', async () => {
    vi.mocked(getPopupAnnouncements).mockResolvedValue([first])
    const user = userEvent.setup()

    renderPopup()
    const dialog = await screen.findByRole('dialog', { name: 'Announcement' })
    await user.keyboard('{Escape}')

    expect(dialog).toBeInTheDocument()
    expect(screen.getByText('Maintenance tonight')).toBeInTheDocument()
    expect(screen.queryByRole('button', { name: 'Close' })).toBeNull()
    expect(screen.getAllByRole('button')).toHaveLength(1)
  })

  it('confirms the current announcement, then shows the next one and marks it read', async () => {
    vi.mocked(getPopupAnnouncements).mockResolvedValue([first, second])
    vi.mocked(ackPopupAnnouncement).mockResolvedValue()
    const user = userEvent.setup()

    renderPopup()
    expect(await screen.findByText('Maintenance tonight')).toBeInTheDocument()
    expect(screen.getByText(/1 more to read/)).toBeInTheDocument()
    await user.click(screen.getByRole('button', { name: 'I have read it' }))

    expect(await screen.findByText('New models available')).toBeInTheDocument()
    expect(ackPopupAnnouncement).toHaveBeenCalledWith(first.id)
    expect(
      useNotificationStore.getState().isAnnouncementRead(`id:${first.id}`)
    ).toBe(true)
    expect(screen.queryByText(/more to read/)).not.toBeInTheDocument()

    await user.click(screen.getByRole('button', { name: 'I have read it' }))
    await waitFor(() =>
      expect(screen.queryByRole('dialog')).not.toBeInTheDocument()
    )
    expect(ackPopupAnnouncement).toHaveBeenLastCalledWith(second.id)
  })

  it('reports a failed confirmation and lets the user continue for this session', async () => {
    vi.mocked(getPopupAnnouncements).mockResolvedValue([first])
    vi.mocked(ackPopupAnnouncement).mockRejectedValue(new Error('offline'))
    const user = userEvent.setup()

    renderPopup()
    await user.click(
      await screen.findByRole('button', { name: 'I have read it' })
    )

    await waitFor(() =>
      expect(screen.queryByRole('dialog')).not.toBeInTheDocument()
    )
    expect(handleServerError).toHaveBeenCalledTimes(1)
    expect(
      useNotificationStore.getState().isAnnouncementRead(`id:${first.id}`)
    ).toBe(false)
  })
})
