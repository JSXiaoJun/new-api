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
/* eslint-disable react-refresh/only-export-components */
import { useQueryClient } from '@tanstack/react-query'
import type { OnChangeFn, SortingState } from '@tanstack/react-table'
import React, {
  createContext,
  useContext,
  useState,
  useCallback,
  useMemo,
  useEffect,
} from 'react'

import { useChannelUpstreamUpdates } from '../hooks/use-channel-upstream-updates'
import { channelsQueryKeys } from '../lib'
import type { Channel } from '../types'

// ============================================================================
// Types
// ============================================================================

type DialogType =
  | 'create-channel'
  | 'update-channel'
  | 'test-channel'
  | 'balance-query'
  | 'fetch-models'
  | 'ollama-models'
  | 'inference-status'
  | 'multi-key-manage'
  | 'tag-batch-edit'
  | 'edit-tag'
  | 'copy-channel'
  | null

type UpstreamUpdateState = ReturnType<typeof useChannelUpstreamUpdates>

type UpstreamUpdateActions = Pick<
  UpstreamUpdateState,
  'openModal' | 'detectChannelUpdates'
>

// Dialog state lives in its own context: opening or closing a dialog must not
// re-render every channel row, card, and cell that only needs the setters.
type ChannelsDialogContextType = {
  open: DialogType
  currentRow: Channel | null
  currentTag: string | null
  upstream: UpstreamUpdateState
}

type ChannelsContextType = {
  setOpen: (open: DialogType) => void
  setCurrentRow: (row: Channel | null) => void
  setCurrentTag: (tag: string | null) => void
  enableTagMode: boolean
  setEnableTagMode: (enabled: boolean) => void
  sorting: SortingState
  setSorting: OnChangeFn<SortingState>
  batchMode: boolean
  setBatchMode: (enabled: boolean) => void
  sensitiveVisible: boolean
  setSensitiveVisible: (visible: boolean) => void
  priorityActionContainer: HTMLDivElement | null
  setPriorityActionContainer: (container: HTMLDivElement | null) => void
  upstreamActions: UpstreamUpdateActions
}

// ============================================================================
// Context
// ============================================================================

const ChannelsContext = createContext<ChannelsContextType | undefined>(
  undefined
)

const ChannelsDialogContext = createContext<
  ChannelsDialogContextType | undefined
>(undefined)

// ============================================================================
// Provider
// ============================================================================

export function ChannelsProvider({ children }: { children: React.ReactNode }) {
  const [open, setOpen] = useState<DialogType>(null)
  const [currentRow, setCurrentRow] = useState<Channel | null>(null)
  const [currentTag, setCurrentTag] = useState<string | null>(null)
  const [enableTagMode, setEnableTagMode] = useState(() => {
    return localStorage.getItem('enable-tag-mode') === 'true'
  })
  const [sorting, setSorting] = useState<SortingState>(() => [
    {
      id:
        localStorage.getItem('channels-id-sort') === 'true' ? 'id' : 'priority',
      desc: true,
    },
  ])
  const idSort = sorting[0]?.id === 'id'
  useEffect(() => {
    // Retain the existing ID-sort preference for returning users.
    localStorage.setItem('channels-id-sort', String(idSort))
  }, [idSort])
  const [batchMode, setBatchMode] = useState(false)
  const [sensitiveVisible, setSensitiveVisible] = useState(true)
  const [priorityActionContainer, setPriorityActionContainer] =
    useState<HTMLDivElement | null>(null)

  const queryClient = useQueryClient()
  const refreshChannels = useCallback(async () => {
    await queryClient.invalidateQueries({ queryKey: channelsQueryKeys.all })
  }, [queryClient])
  const upstream = useChannelUpstreamUpdates(refreshChannels)
  const { openModal, detectChannelUpdates } = upstream
  const upstreamActions = useMemo<UpstreamUpdateActions>(
    () => ({ openModal, detectChannelUpdates }),
    [openModal, detectChannelUpdates]
  )

  // useState setters are stable, so the context value only needs to change when
  // an actual list state value changes. Memoizing avoids handing every consumer
  // (including all channel cards/cells) a brand-new object on each render.
  const value = useMemo<ChannelsContextType>(
    () => ({
      setOpen,
      setCurrentRow,
      setCurrentTag,
      enableTagMode,
      setEnableTagMode,
      sorting,
      setSorting,
      batchMode,
      setBatchMode,
      sensitiveVisible,
      setSensitiveVisible,
      priorityActionContainer,
      setPriorityActionContainer,
      upstreamActions,
    }),
    [
      enableTagMode,
      sorting,
      batchMode,
      sensitiveVisible,
      priorityActionContainer,
      upstreamActions,
    ]
  )

  const dialogValue = useMemo<ChannelsDialogContextType>(
    () => ({ open, currentRow, currentTag, upstream }),
    [open, currentRow, currentTag, upstream]
  )

  return (
    <ChannelsContext.Provider value={value}>
      <ChannelsDialogContext.Provider value={dialogValue}>
        {children}
      </ChannelsDialogContext.Provider>
    </ChannelsContext.Provider>
  )
}

// ============================================================================
// Hook
// ============================================================================

export function useChannels() {
  const context = useContext(ChannelsContext)
  if (!context) {
    throw new Error('useChannels must be used within ChannelsProvider')
  }
  return context
}

/**
 * Open dialog, its target row/tag, and upstream update state. Read it only in
 * dialogs and other single components; rows should use `useChannels()`.
 */
export function useChannelsDialog() {
  const context = useContext(ChannelsDialogContext)
  if (!context) {
    throw new Error('useChannelsDialog must be used within ChannelsProvider')
  }
  return context
}
