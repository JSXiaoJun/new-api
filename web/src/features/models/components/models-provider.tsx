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
import React, { createContext, useContext, useMemo, useState } from 'react'

import type {
  Model,
  ModelTabCategory,
  Vendor,
  SyncLocale,
  SyncSource,
} from '../types'

// ============================================================================
// Types
// ============================================================================

type DialogType =
  | 'create-model'
  | 'update-model'
  | 'price-model'
  | 'create-vendor'
  | 'vendors'
  | 'price-sync'
  | 'update-vendor'
  | 'missing-models'
  | 'sync-wizard'
  | 'prefill-groups'
  | 'description'
  | null

type DescriptionData = { modelName: string; description: string }

// Dialog state lives in its own context: opening or closing a dialog must not
// re-render the model table rows that only need the setters.
type ModelsDialogContextType = {
  open: DialogType
  currentRow: Model | null
  currentVendor: Vendor | null
  descriptionData: DescriptionData | null
}

type ModelsContextType = {
  setOpen: (open: DialogType) => void
  setCurrentRow: (model: Model | null) => void
  setCurrentVendor: (vendor: Vendor | null) => void
  selectedVendor: string | null
  setSelectedVendor: (vendor: string | null) => void
  setDescriptionData: (
    data: { modelName: string; description: string } | null
  ) => void
  syncWizardOptions: { locale: SyncLocale; source: SyncSource }
  setSyncWizardOptions: React.Dispatch<
    React.SetStateAction<{ locale: SyncLocale; source: SyncSource }>
  >
  tabCategory: ModelTabCategory
  setTabCategory: (category: ModelTabCategory) => void
}

// ============================================================================
// Context
// ============================================================================

const ModelsContext = createContext<ModelsContextType | undefined>(undefined)

const ModelsDialogContext = createContext<ModelsDialogContextType | undefined>(
  undefined
)

// ============================================================================
// Provider
// ============================================================================

export function ModelsProvider({ children }: { children: React.ReactNode }) {
  const [open, setOpen] = useState<DialogType>(null)
  const [currentRow, setCurrentRow] = useState<Model | null>(null)
  const [currentVendor, setCurrentVendor] = useState<Vendor | null>(null)
  const [selectedVendor, setSelectedVendor] = useState<string | null>(null)
  const [descriptionData, setDescriptionData] =
    useState<DescriptionData | null>(null)
  const [syncWizardOptions, setSyncWizardOptions] = useState<{
    locale: SyncLocale
    source: SyncSource
  }>({
    locale: 'zh',
    source: 'official',
  })
  const [tabCategory, setTabCategory] = useState<ModelTabCategory>('metadata')

  const value = useMemo<ModelsContextType>(
    () => ({
      setOpen,
      setCurrentRow,
      setCurrentVendor,
      selectedVendor,
      setSelectedVendor,
      setDescriptionData,
      syncWizardOptions,
      setSyncWizardOptions,
      tabCategory,
      setTabCategory,
    }),
    [selectedVendor, syncWizardOptions, tabCategory]
  )

  const dialogValue = useMemo<ModelsDialogContextType>(
    () => ({ open, currentRow, currentVendor, descriptionData }),
    [open, currentRow, currentVendor, descriptionData]
  )

  return (
    <ModelsContext.Provider value={value}>
      <ModelsDialogContext.Provider value={dialogValue}>
        {children}
      </ModelsDialogContext.Provider>
    </ModelsContext.Provider>
  )
}

// ============================================================================
// Hook
// ============================================================================

export function useModels() {
  const context = useContext(ModelsContext)
  if (!context) {
    throw new Error('useModels must be used within ModelsProvider')
  }
  return context
}

/**
 * Open dialog and its target model/vendor/description. Read it only in dialogs;
 * table rows should use `useModels()`.
 */
export function useModelsDialog() {
  const context = useContext(ModelsDialogContext)
  if (!context) {
    throw new Error('useModelsDialog must be used within ModelsProvider')
  }
  return context
}
