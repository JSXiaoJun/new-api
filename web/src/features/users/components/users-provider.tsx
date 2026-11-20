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
  SecureVerificationDialog,
  useSecureVerification,
} from '@/features/auth/secure-verification'
import useDialogState from '@/hooks/use-dialog'

import type { User, UsersDialogType } from '../types'

// Dialog state lives in its own context: opening or closing a dialog must not
// re-render every user row that only needs the setters.
type UsersDialogContextType = {
  open: UsersDialogType | null
  currentRow: User | null
}

type UsersContextType = {
  setOpen: (str: UsersDialogType | null) => void
  setCurrentRow: React.Dispatch<React.SetStateAction<User | null>>
  refreshTrigger: number
  triggerRefresh: () => void
  /** Step-up ceremony shared by every risky user-management action. */
  requestVerification: ReturnType<
    typeof useSecureVerification
  >['requestVerification']
  /** True while the step-up dialog is showing; confirm dialogs yield to it. */
  verificationActive: boolean
}

const UsersContext = React.createContext<UsersContextType | null>(null)

const UsersDialogContext = React.createContext<UsersDialogContextType | null>(
  null
)

export function UsersProvider({ children }: { children: React.ReactNode }) {
  const [open, setOpen] = useDialogState<UsersDialogType>(null)
  const [currentRow, setCurrentRow] = useState<User | null>(null)
  const [refreshTrigger, setRefreshTrigger] = useState(0)
  // The dialog mounts once here; consumers only receive the proof token, and
  // the memoized value keeps dialog keystrokes from re-rendering the table.
  const verification = useSecureVerification()
  const requestVerification = verification.requestVerification
  const verificationActive = verification.isActive

  const triggerRefresh = useCallback(
    () => setRefreshTrigger((prev) => prev + 1),
    []
  )

  const value = useMemo<UsersContextType>(
    () => ({
      setOpen,
      setCurrentRow,
      refreshTrigger,
      triggerRefresh,
      requestVerification,
      verificationActive,
    }),
    [
      setOpen,
      refreshTrigger,
      triggerRefresh,
      requestVerification,
      verificationActive,
    ]
  )

  const dialogValue = useMemo<UsersDialogContextType>(
    () => ({ open, currentRow }),
    [open, currentRow]
  )

  return (
    <UsersContext value={value}>
      <UsersDialogContext value={dialogValue}>{children}</UsersDialogContext>
      <SecureVerificationDialog {...verification.dialogProps} />
    </UsersContext>
  )
}

// eslint-disable-next-line react-refresh/only-export-components
export const useUsers = () => {
  const usersContext = React.useContext(UsersContext)

  if (!usersContext) {
    throw new Error('useUsers has to be used within <UsersContext>')
  }

  return usersContext
}

/**
 * Open dialog and its target user. Read it only in dialogs; table rows should
 * use `useUsers()`.
 */
// eslint-disable-next-line react-refresh/only-export-components
export const useUsersDialog = () => {
  const context = React.useContext(UsersDialogContext)

  if (!context) {
    throw new Error('useUsersDialog has to be used within <UsersProvider>')
  }

  return context
}
