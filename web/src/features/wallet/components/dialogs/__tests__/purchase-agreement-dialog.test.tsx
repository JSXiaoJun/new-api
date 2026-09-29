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
import { render, screen, waitFor } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { afterEach, describe, expect, it, vi } from 'vitest'

import { confirmPurchaseAgreement } from '../../../api'
import { PurchaseAgreementDialog } from '../purchase-agreement-dialog'

vi.mock('../../../api', () => ({
  confirmPurchaseAgreement: vi.fn(),
  isApiSuccess: (response: { success?: boolean }) => response.success === true,
}))

vi.mock('sonner', () => ({
  toast: { success: vi.fn(), error: vi.fn() },
}))

const PHRASE = 'I understand and confirm'

async function completeForm(user: ReturnType<typeof userEvent.setup>) {
  for (const checkbox of screen.getAllByRole('checkbox')) {
    await user.click(checkbox)
  }
  await user.type(
    screen.getByRole('textbox', { name: 'Confirmation text' }),
    PHRASE
  )
}

describe('PurchaseAgreementDialog', () => {
  afterEach(() => {
    vi.clearAllMocks()
  })

  it('keeps submit disabled until all three items are checked and the phrase is typed', async () => {
    const user = userEvent.setup()
    render(
      <PurchaseAgreementDialog
        open
        onOpenChange={vi.fn()}
        onConfirmed={vi.fn()}
      />
    )
    const submit = screen.getByRole('button', { name: 'Confirm and Continue' })

    const checkboxes = screen.getAllByRole('checkbox')
    expect(checkboxes).toHaveLength(3)
    await user.click(checkboxes[0])
    await user.click(checkboxes[1])
    await user.type(
      screen.getByRole('textbox', { name: 'Confirmation text' }),
      PHRASE
    )
    // The no-invoice item is still unchecked
    expect(submit).toBeDisabled()

    await user.click(checkboxes[2])
    expect(submit).toBeEnabled()
  })

  it('marks the input invalid and blocks submit when the phrase does not match', async () => {
    const user = userEvent.setup()
    render(
      <PurchaseAgreementDialog
        open
        onOpenChange={vi.fn()}
        onConfirmed={vi.fn()}
      />
    )

    for (const checkbox of screen.getAllByRole('checkbox')) {
      await user.click(checkbox)
    }
    const input = screen.getByRole('textbox', { name: 'Confirmation text' })
    await user.type(input, 'I agree')

    expect(input).toHaveAttribute('aria-invalid', 'true')
    expect(
      screen.getByRole('button', { name: 'Confirm and Continue' })
    ).toBeDisabled()
  })

  it('submits the confirmation and reports the signed time on success', async () => {
    vi.mocked(confirmPurchaseAgreement).mockResolvedValue({
      success: true,
      data: { purchase_agreement_at: 1_700_000_000 },
    })
    const onConfirmed = vi.fn()
    const user = userEvent.setup()
    render(
      <PurchaseAgreementDialog
        open
        onOpenChange={vi.fn()}
        onConfirmed={onConfirmed}
      />
    )

    await completeForm(user)
    await user.click(
      screen.getByRole('button', { name: 'Confirm and Continue' })
    )
    await user.click(
      await screen.findByRole('button', { name: 'Agree and top up' })
    )

    await waitFor(() => expect(onConfirmed).toHaveBeenCalledWith(1_700_000_000))
    expect(confirmPurchaseAgreement).toHaveBeenCalledWith({
      confirm_not_mainland_citizen: true,
      confirm_not_in_mainland: true,
      confirm_no_invoice: true,
      statement: PHRASE,
    })
  })

  it('asks for a second confirmation and submits nothing when the user reviews again', async () => {
    const user = userEvent.setup()
    render(
      <PurchaseAgreementDialog
        open
        onOpenChange={vi.fn()}
        onConfirmed={vi.fn()}
      />
    )

    await completeForm(user)
    await user.click(
      screen.getByRole('button', { name: 'Confirm and Continue' })
    )

    expect(
      await screen.findByRole('alertdialog', {
        name: 'Confirm agreement to the top-up terms?',
      })
    ).toBeInTheDocument()
    expect(confirmPurchaseAgreement).not.toHaveBeenCalled()

    await user.click(screen.getByRole('button', { name: 'Review again' }))
    await waitFor(() =>
      expect(screen.queryByRole('alertdialog')).not.toBeInTheDocument()
    )
    expect(confirmPurchaseAgreement).not.toHaveBeenCalled()
    // The form stays filled in so the user can continue without retyping
    expect(
      screen.getByRole('textbox', { name: 'Confirmation text' })
    ).toHaveValue(PHRASE)
  })

  it('does not report confirmation when the server rejects it', async () => {
    vi.mocked(confirmPurchaseAgreement).mockResolvedValue({
      success: false,
      message: 'rejected',
    })
    const onConfirmed = vi.fn()
    const user = userEvent.setup()
    render(
      <PurchaseAgreementDialog
        open
        onOpenChange={vi.fn()}
        onConfirmed={onConfirmed}
      />
    )

    await completeForm(user)
    await user.click(
      screen.getByRole('button', { name: 'Confirm and Continue' })
    )
    await user.click(
      await screen.findByRole('button', { name: 'Agree and top up' })
    )

    await waitFor(() => expect(confirmPurchaseAgreement).toHaveBeenCalled())
    expect(onConfirmed).not.toHaveBeenCalled()
    expect(
      screen.getByRole('button', { name: 'Confirm and Continue' })
    ).toBeEnabled()
  })

  it('shows a signed agreement as checked, read-only, and closable', async () => {
    const onOpenChange = vi.fn()
    const user = userEvent.setup()
    render(
      <PurchaseAgreementDialog
        open
        onOpenChange={onOpenChange}
        signedAt={1_700_000_000}
        onConfirmed={vi.fn()}
      />
    )

    for (const checkbox of screen.getAllByRole('checkbox')) {
      expect(checkbox).toHaveAttribute('aria-checked', 'true')
      expect(checkbox).toHaveAttribute('aria-disabled', 'true')
    }
    const input = screen.getByRole('textbox', { name: 'Confirmation text' })
    expect(input).toHaveValue(PHRASE)
    expect(input).toBeDisabled()
    expect(
      screen.queryByRole('button', { name: 'Confirm and Continue' })
    ).not.toBeInTheDocument()

    // Footer close button renders before the corner X icon button
    const [footerClose] = screen.getAllByRole('button', { name: 'Close' })
    await user.click(footerClose)
    expect(onOpenChange).toHaveBeenCalledWith(false)
  })
})
