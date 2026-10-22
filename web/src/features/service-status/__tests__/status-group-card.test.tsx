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

import { StatusGroupCard } from '../components/status-group-card'
import type { LiveGroupStatus } from '../types'

const group: LiveGroupStatus = {
  group: 'pro',
  description: 'Pro group',
  short_success_rate: 99.5,
  window_success_rate: 97,
  models: [
    {
      model_name: 'gpt-test',
      short_success_rate: 99.5,
      window_success_rate: 97,
      minutes: [
        { ts: 60, success_rate: 100 },
        { ts: 120, success_rate: null },
      ],
    },
    {
      model_name: 'rare-model',
      short_success_rate: null,
      window_success_rate: null,
      minutes: [],
    },
  ],
}

function renderCard() {
  render(
    <StatusGroupCard
      group={group}
      windowStart={0}
      windowEnd={180}
      bucketSeconds={60}
      shortWindowMinutes={5}
    />
  )
}

describe('service status group card', () => {
  test('shows the group success rates for the short window and the hour', () => {
    renderCard()

    expect(screen.getByText('Pro group')).toBeInTheDocument()
    expect(screen.getAllByText('99.50%').length).toBeGreaterThan(0)
    expect(screen.getAllByText('97.00%').length).toBeGreaterThan(0)
  })

  test('draws one strip slot per minute with its state', () => {
    renderCard()

    const strip = screen.getByRole('img', {
      name: 'Success rate of gpt-test per minute over the last hour',
    })
    const states = [...strip.children].map((slot) =>
      slot.getAttribute('data-state')
    )
    expect(states).toEqual(['idle', 'rated', 'sparse', 'idle'])
  })

  test('shows a dash for a model with too few requests to rate', () => {
    renderCard()

    const row = screen.getByText('rare-model').closest('li')
    expect(row).not.toBeNull()
    expect(within(row as HTMLElement).getAllByText('—')).toHaveLength(2)
  })
})
