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

import {
  getOptionValue,
  useSystemOptions,
} from '@/features/system-settings/hooks/use-system-options'
import useDialogState from '@/hooks/use-dialog'

import type { PlanRecord, SubscriptionsDialogType } from '../types'

const CURRENT_COMPLIANCE_TERMS_VERSION = 'v1'

// Dialog state lives in its own context: opening or closing a dialog must not
// re-render every plan row that only needs the setters.
type SubscriptionsDialogContextType = {
  open: SubscriptionsDialogType | null
  currentRow: PlanRecord | null
}

type SubscriptionsContextType = {
  setOpen: (str: SubscriptionsDialogType | null) => void
  setCurrentRow: React.Dispatch<React.SetStateAction<PlanRecord | null>>
  refreshTrigger: number
  triggerRefresh: () => void
  complianceConfirmed: boolean
}

const SubscriptionsContext =
  React.createContext<SubscriptionsContextType | null>(null)

const SubscriptionsDialogContext =
  React.createContext<SubscriptionsDialogContextType | null>(null)

export function SubscriptionsProvider({
  children,
}: {
  children: React.ReactNode
}) {
  const [open, setOpen] = useDialogState<SubscriptionsDialogType>(null)
  const [currentRow, setCurrentRow] = useState<PlanRecord | null>(null)
  const [refreshTrigger, setRefreshTrigger] = useState(0)
  const { data } = useSystemOptions()
  const complianceOptions = getOptionValue(data?.data, {
    'payment_setting.compliance_confirmed': false,
    'payment_setting.compliance_terms_version': '',
  })
  const complianceConfirmed =
    complianceOptions['payment_setting.compliance_confirmed'] &&
    complianceOptions['payment_setting.compliance_terms_version'] ===
      CURRENT_COMPLIANCE_TERMS_VERSION

  const triggerRefresh = useCallback(
    () => setRefreshTrigger((prev) => prev + 1),
    []
  )

  const value = useMemo<SubscriptionsContextType>(
    () => ({
      setOpen,
      setCurrentRow,
      refreshTrigger,
      triggerRefresh,
      complianceConfirmed,
    }),
    [setOpen, refreshTrigger, triggerRefresh, complianceConfirmed]
  )

  const dialogValue = useMemo<SubscriptionsDialogContextType>(
    () => ({ open, currentRow }),
    [open, currentRow]
  )

  return (
    <SubscriptionsContext value={value}>
      <SubscriptionsDialogContext value={dialogValue}>
        {children}
      </SubscriptionsDialogContext>
    </SubscriptionsContext>
  )
}

// eslint-disable-next-line react-refresh/only-export-components
export const useSubscriptions = () => {
  const ctx = React.useContext(SubscriptionsContext)
  if (!ctx) {
    throw new Error(
      'useSubscriptions has to be used within <SubscriptionsProvider>'
    )
  }
  return ctx
}

/**
 * Open dialog and its target plan. Read it only in dialogs; table rows should
 * use `useSubscriptions()`.
 */
// eslint-disable-next-line react-refresh/only-export-components
export const useSubscriptionsDialog = () => {
  const ctx = React.useContext(SubscriptionsDialogContext)
  if (!ctx) {
    throw new Error(
      'useSubscriptionsDialog has to be used within <SubscriptionsProvider>'
    )
  }
  return ctx
}
