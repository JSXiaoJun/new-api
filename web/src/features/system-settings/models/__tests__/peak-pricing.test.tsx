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
import { act, fireEvent, render, screen, within } from '@testing-library/react'
import { createRef, useState } from 'react'
import { describe, expect, it, vi } from 'vitest'

import {
  ModelPricingEditorPanel,
  type ModelPricingEditorPanelHandle,
} from '../model-pricing-sheet'
import {
  buildModelSnapshots,
  getSnapshotSignature,
  isBasePricingUnset,
} from '../model-pricing-snapshots'
import {
  createPeakPricing,
  peakPricingSchema,
  type PeakPricing,
} from '../peak-pricing'
import { PeakPricingEditor } from '../peak-pricing-editor'

function schedule(): PeakPricing {
  return {
    timezone: 'Asia/Shanghai',
    default: { mode: 'per_request', price: 1 },
    periods: [
      {
        start: '22:00',
        end: '06:00',
        tariff: { mode: 'per_second', price: 0.2 },
      },
      {
        start: '09:00',
        end: '18:00',
        tariff: { mode: 'per_token', input_price: 2, output_price: 6 },
      },
    ],
  }
}

describe('peak pricing validation', () => {
  it('accepts non-overlapping overnight periods and explicit free prices', () => {
    const value = schedule()
    value.default.price = 0
    expect(peakPricingSchema.parse(value).default.price).toBe(0)
  })

  it('rejects overnight overlaps, identical endpoints, missing prices and negative prices', () => {
    const overlap = schedule()
    overlap.periods[1].start = '05:00'
    expect(peakPricingSchema.safeParse(overlap).success).toBe(false)
    const empty = schedule()
    empty.periods[0].end = empty.periods[0].start
    expect(peakPricingSchema.safeParse(empty).success).toBe(false)
    expect(peakPricingSchema.safeParse(createPeakPricing()).success).toBe(false)
    const negative = schedule()
    negative.default.price = -1
    expect(peakPricingSchema.safeParse(negative).success).toBe(false)
  })

  it('preserves peak-only models and detects changed period prices in snapshots', () => {
    const value = schedule()
    const input = {
      modelPrice: '{}',
      modelRatio: '{}',
      cacheRatio: '{}',
      createCacheRatio: '{}',
      completionRatio: '{}',
      imageRatio: '{}',
      audioRatio: '{}',
      audioCompletionRatio: '{}',
      billingMode: '{}',
      billingExpr: '{}',
      peakPricing: JSON.stringify({ video: value }),
    }
    const [snapshot] = buildModelSnapshots(input)
    expect(snapshot.billingMode).toBe('peak')
    expect(isBasePricingUnset(snapshot)).toBe(false)
    const before = getSnapshotSignature(snapshot)
    value.periods[0].tariff.price = 0.3
    const [updated] = buildModelSnapshots({
      ...input,
      peakPricing: JSON.stringify({ video: value }),
    })
    expect(getSnapshotSignature(updated)).not.toBe(before)
  })
})

function EditableSchedule() {
  const [value, setValue] = useState(schedule)
  return (
    <PeakPricingEditor
      value={value}
      onChange={setValue}
      modelName='video-model'
    />
  )
}

describe('peak pricing editor', () => {
  it('adds and removes periods when randomUUID is unavailable on HTTP', () => {
    vi.stubGlobal('crypto', { randomUUID: undefined })
    try {
      render(<EditableSchedule />)
      fireEvent.click(screen.getByRole('button', { name: 'Add time period' }))
      const period = screen.getByRole('region', { name: 'Time period 3' })
      fireEvent.click(
        within(period).getByRole('button', { name: 'Delete time period' })
      )
      expect(
        screen.queryByRole('region', { name: 'Time period 3' })
      ).not.toBeInTheDocument()
      expect(
        screen.getByRole('region', { name: 'Time period 2' })
      ).toBeInTheDocument()
    } finally {
      vi.unstubAllGlobals()
    }
  })

  it('commits both the expression and request multiplier after a raw edit', async () => {
    const ref = createRef<ModelPricingEditorPanelHandle>()
    const value = schedule()
    value.default = { mode: 'tiered_expr', expression: 'p + c + 1' }
    render(
      <ModelPricingEditorPanel
        ref={ref}
        editData={{
          name: 'peak-expression',
          billingMode: 'peak',
          peakPricing: value,
        }}
      />
    )
    const region = screen.getByRole('region', { name: 'Default pricing' })
    const expression =
      '(p * 3 + c * 9 + 1) * (hour("Asia/Shanghai") >= 9 ? 2 : 1)'
    fireEvent.change(within(region).getByRole('textbox'), {
      target: { value: expression },
    })
    await act(async () => {
      expect(await ref.current?.commitDraft()).toMatchObject({
        peakPricing: { default: { mode: 'tiered_expr', expression } },
      })
    })
  })

  it('adds and deletes time periods and selects each of the four billing modes', () => {
    render(<EditableSchedule />)
    fireEvent.click(screen.getByRole('button', { name: 'Add time period' }))
    const period = screen.getByRole('region', { name: 'Time period 3' })
    const mode = within(period).getByLabelText('Billing mode')
    expect(within(mode).getAllByRole('option')).toHaveLength(4)
    fireEvent.change(mode, { target: { value: 'per_token' } })
    expect(
      within(period).getByLabelText('Input price ($/1M)')
    ).toBeInTheDocument()
    fireEvent.change(mode, { target: { value: 'per_second' } })
    expect(
      within(period).getByLabelText('Price per second ($)')
    ).toBeInTheDocument()
    fireEvent.click(
      within(period).getByRole('button', { name: 'Delete time period' })
    )
    expect(
      screen.queryByRole('region', { name: 'Time period 3' })
    ).not.toBeInTheDocument()
  })

  it('commits a peak draft and blocks a changed overlapping period', async () => {
    const ref = createRef<ModelPricingEditorPanelHandle>()
    render(
      <ModelPricingEditorPanel
        ref={ref}
        editData={{
          name: 'peak-model',
          billingMode: 'peak',
          peakPricing: schedule(),
        }}
      />
    )
    expect(screen.getByRole('tab', { name: 'Peak pricing' })).toHaveAttribute(
      'aria-selected',
      'true'
    )
    const tabList = screen.getByRole('tablist')
    expect(tabList).toHaveClass('group-data-horizontal/tabs:h-auto')
    expect(tabList).toHaveClass('grid-cols-2')
    await act(async () => {
      expect(await ref.current?.commitDraft()).toMatchObject({
        billingMode: 'peak',
        peakPricing: schedule(),
      })
    })
    const period = screen.getByRole('region', { name: 'Time period 2' })
    fireEvent.change(within(period).getByLabelText('Start time'), {
      target: { value: '05:30' },
    })
    await act(async () => {
      expect(await ref.current?.commitDraft()).toBeNull()
    })
    expect(screen.getByRole('alert')).toHaveTextContent(
      'Time periods must not overlap.'
    )
  })
})
