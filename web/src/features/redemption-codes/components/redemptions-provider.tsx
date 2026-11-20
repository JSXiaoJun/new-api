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
import React, { useCallback, useMemo, useState } from 'react'

import useDialogState from '@/hooks/use-dialog'

import type { Redemption, RedemptionsDialogType } from '../types'

// Dialog state lives in its own context: opening or closing a dialog must not
// re-render every redemption row that only needs the setters.
type RedemptionsDialogContextType = {
  open: RedemptionsDialogType | null
  currentRow: Redemption | null
}

type RedemptionsContextType = {
  setOpen: (str: RedemptionsDialogType | null) => void
  setCurrentRow: React.Dispatch<React.SetStateAction<Redemption | null>>
  refreshTrigger: number
  triggerRefresh: () => void
}

const RedemptionsContext = React.createContext<RedemptionsContextType | null>(
  null
)

const RedemptionsDialogContext =
  React.createContext<RedemptionsDialogContextType | null>(null)

export function RedemptionsProvider({
  children,
}: {
  children: React.ReactNode
}) {
  const [open, setOpen] = useDialogState<RedemptionsDialogType>(null)
  const [currentRow, setCurrentRow] = useState<Redemption | null>(null)
  const [refreshTrigger, setRefreshTrigger] = useState(0)

  const triggerRefresh = useCallback(
    () => setRefreshTrigger((prev) => prev + 1),
    []
  )

  const value = useMemo<RedemptionsContextType>(
    () => ({ setOpen, setCurrentRow, refreshTrigger, triggerRefresh }),
    [setOpen, refreshTrigger, triggerRefresh]
  )

  const dialogValue = useMemo<RedemptionsDialogContextType>(
    () => ({ open, currentRow }),
    [open, currentRow]
  )

  return (
    <RedemptionsContext value={value}>
      <RedemptionsDialogContext value={dialogValue}>
        {children}
      </RedemptionsDialogContext>
    </RedemptionsContext>
  )
}

// eslint-disable-next-line react-refresh/only-export-components
export const useRedemptions = () => {
  const redemptionsContext = React.useContext(RedemptionsContext)

  if (!redemptionsContext) {
    throw new Error(
      'useRedemptions has to be used within <RedemptionsProvider>'
    )
  }

  return redemptionsContext
}

/**
 * Open dialog and its target code. Read it only in dialogs; table rows should
 * use `useRedemptions()`.
 */
// eslint-disable-next-line react-refresh/only-export-components
export const useRedemptionsDialog = () => {
  const context = React.useContext(RedemptionsDialogContext)

  if (!context) {
    throw new Error(
      'useRedemptionsDialog has to be used within <RedemptionsProvider>'
    )
  }

  return context
}
