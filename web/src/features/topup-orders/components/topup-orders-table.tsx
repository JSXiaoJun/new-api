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
import { useQuery } from '@tanstack/react-query'
import type { OnChangeFn, PaginationState } from '@tanstack/react-table'
import { useState } from 'react'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'

import { DataTablePage, useDataTable } from '@/components/data-table'
import { useMediaQuery } from '@/hooks'

import { getTopUpOrders } from '../api'
import type { TopUpOrder } from '../types'
import { useTopUpOrdersColumns } from './topup-orders-columns'

export function TopUpOrdersTable() {
  const { t } = useTranslation()
  const columns = useTopUpOrdersColumns()
  const isMobile = useMediaQuery('(max-width: 640px)')
  const [globalFilter, setGlobalFilter] = useState('')
  const [pagination, setPagination] = useState<PaginationState>({
    pageIndex: 0,
    pageSize: isMobile ? 10 : 20,
  })

  const onGlobalFilterChange: OnChangeFn<string> = (updater) => {
    setGlobalFilter((current) =>
      typeof updater === 'function' ? updater(current) : updater
    )
    setPagination((current) => ({ ...current, pageIndex: 0 }))
  }

  const { data, isLoading, isFetching } = useQuery({
    queryKey: [
      'topup-orders',
      pagination.pageIndex + 1,
      pagination.pageSize,
      globalFilter,
    ],
    queryFn: async () => {
      const result = await getTopUpOrders({
        p: pagination.pageIndex + 1,
        page_size: pagination.pageSize,
        keyword: globalFilter,
      })
      if (!result.success) {
        toast.error(result.message || t('Failed to load recharge orders'))
        return { items: [], total: 0 }
      }
      return {
        items: result.data?.items ?? [],
        total: result.data?.total ?? 0,
      }
    },
    placeholderData: (previousData) => previousData,
  })

  const { table } = useDataTable<TopUpOrder>({
    data: data?.items ?? [],
    columns,
    globalFilter,
    pagination,
    onGlobalFilterChange,
    onPaginationChange: setPagination,
    manualFiltering: true,
    manualPagination: true,
    totalCount: data?.total ?? 0,
  })

  return (
    <DataTablePage
      table={table}
      columns={columns}
      isLoading={isLoading}
      isFetching={isFetching}
      emptyTitle={t('No recharge orders found')}
      emptyDescription={t('No recharge orders are available.')}
      skeletonKeyPrefix='topup-orders-skeleton'
      applyHeaderSize
      toolbarProps={{
        searchPlaceholder: t('Search by order number...'),
        searchDebounceMs: 500,
      }}
    />
  )
}
