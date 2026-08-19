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

export type TopUpOrderStatus = 'success' | 'pending' | 'failed' | 'expired'

export type TopUpOrder = {
  id: number
  user_id: number
  username: string
  display_name: string
  email: string
  amount: number
  money: number
  trade_no: string
  payment_method: string
  payment_provider: string
  create_time: number
  complete_time: number
  status: TopUpOrderStatus
}

export type GetTopUpOrdersParams = {
  p?: number
  page_size?: number
  keyword?: string
}

export type GetTopUpOrdersResponse = {
  success: boolean
  message?: string
  data?: {
    items: TopUpOrder[]
    total: number
    page: number
    page_size: number
  }
}
