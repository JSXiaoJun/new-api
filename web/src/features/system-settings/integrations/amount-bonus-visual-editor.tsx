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
import { Plus } from 'lucide-react'
import { useMemo, useState } from 'react'
import { useTranslation } from 'react-i18next'

import { StaticDataTable } from '@/components/data-table/static/static-data-table'
import { StaticRowActions } from '@/components/data-table/static/static-row-actions'
import { StatusBadge } from '@/components/status-badge'
import { Button } from '@/components/ui/button'

import {
  parseAmountBonusTiers,
  serializeAmountBonusTiers,
  type AmountBonusTier,
} from './amount-bonus'
import { AmountBonusDialog } from './amount-bonus-dialog'

type AmountBonusVisualEditorProps = {
  value: string
  onChange: (value: string) => void
}

export function AmountBonusVisualEditor(props: AmountBonusVisualEditorProps) {
  const { t } = useTranslation()
  const [dialogOpen, setDialogOpen] = useState(false)
  const [editData, setEditData] = useState<AmountBonusTier | null>(null)

  const tiers = useMemo(
    () => parseAmountBonusTiers(props.value) ?? [],
    [props.value]
  )

  const handleSave = (data: AmountBonusTier) => {
    const next = tiers.filter(
      (tier) => tier.amount !== data.amount && tier.amount !== editData?.amount
    )
    props.onChange(serializeAmountBonusTiers([...next, data]))
  }

  const handleDelete = (amount: number) => {
    props.onChange(
      serializeAmountBonusTiers(tiers.filter((tier) => tier.amount !== amount))
    )
  }

  return (
    <div className='space-y-4'>
      <div className='flex flex-col gap-3 sm:flex-row sm:items-center sm:justify-between'>
        <p className='text-muted-foreground text-sm'>
          {t('Give extra balance when a top-up reaches an amount')}
        </p>
        <Button
          type='button'
          onClick={(e) => {
            e.preventDefault()
            setEditData(null)
            setDialogOpen(true)
          }}
          size='sm'
          className='w-full sm:w-auto'
        >
          <Plus className='h-4 w-4 sm:mr-2' aria-hidden='true' />
          <span className='sm:inline'>{t('Add bonus tier')}</span>
        </Button>
      </div>

      {tiers.length === 0 ? (
        <div className='text-muted-foreground rounded-lg border border-dashed p-6 text-center text-sm'>
          {t(
            'No bonus tiers configured. Click "Add bonus tier" to get started.'
          )}
        </div>
      ) : (
        <div className='rounded-md border'>
          <StaticDataTable
            className='rounded-none border-0'
            data={tiers}
            getRowKey={(tier) => tier.amount}
            columns={[
              {
                id: 'amount',
                header: t('Minimum recharge amount'),
                cell: (tier) => (
                  <span className='font-mono text-sm'>{tier.amount}</span>
                ),
              },
              {
                id: 'bonus',
                header: t('Bonus'),
                cell: (tier) => (
                  <StatusBadge
                    variant='info'
                    className='font-mono'
                    copyable={false}
                  >
                    {t('+{{percent}}%', { percent: tier.bonusPercent })}
                  </StatusBadge>
                ),
              },
              {
                id: 'actions',
                header: t('Actions'),
                className: 'text-right',
                cellClassName: 'text-right',
                cell: (tier) => (
                  <StaticRowActions
                    editLabel={t('Edit')}
                    deleteLabel={t('Delete')}
                    menuLabel={t('Open menu')}
                    onEdit={() => {
                      setEditData(tier)
                      setDialogOpen(true)
                    }}
                    onDelete={() => handleDelete(tier.amount)}
                  />
                ),
              },
            ]}
          />
        </div>
      )}

      <AmountBonusDialog
        open={dialogOpen}
        onOpenChange={setDialogOpen}
        onSave={handleSave}
        editData={editData}
      />
    </div>
  )
}
