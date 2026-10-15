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
import { render, screen, within } from '@testing-library/react'
import { describe, expect, test } from 'vitest'

import { TaskRequestDetails } from '../task-request-details'

describe('task request details', () => {
  test('given reference images and a prompt, each image links to its original and the prompt keeps line breaks', () => {
    render(
      <TaskRequestDetails
        request={{
          prompt: 'line one\nline two',
          images: ['https://cdn.example/1.png', 'https://cdn.example/2.png'],
          videos: [],
          audios: [],
        }}
      />
    )

    const images = screen.getAllByRole('img')
    expect(images).toHaveLength(2)
    expect(images[0]).toHaveAttribute('src', 'https://cdn.example/1.png')
    const originals = screen.getAllByRole('link', { name: 'View original' })
    expect(originals[1]).toHaveAttribute('href', 'https://cdn.example/2.png')
    expect(originals[1]).toHaveAttribute('rel', 'noopener noreferrer')
    expect(screen.getByText(/line one/)).toHaveClass('whitespace-pre-wrap')
  })

  test('given no reference video, the section says it was not recorded', () => {
    render(
      <TaskRequestDetails
        request={{ prompt: 'p', images: [], videos: [], audios: [] }}
      />
    )

    const videoSection = screen
      .getByRole('heading', { name: 'Reference videos' })
      .closest('section')
    expect(videoSection).not.toBeNull()
    expect(
      within(videoSection as HTMLElement).getByText(
        'Not recorded for this task'
      )
    ).toBeVisible()
    expect(
      screen.queryByRole('heading', { name: 'Reference audios' })
    ).not.toBeInTheDocument()
  })
})
