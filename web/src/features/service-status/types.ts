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
export type LiveMinutePoint = {
  ts: number
  /** Null when the minute had requests, but too few to report a rate. */
  success_rate: number | null
}

export type LiveModelStatus = {
  model_name: string
  short_success_rate: number | null
  window_success_rate: number | null
  minutes: LiveMinutePoint[]
}

export type LiveGroupStatus = {
  group: string
  description: string
  short_success_rate: number | null
  window_success_rate: number | null
  models: LiveModelStatus[]
}

export type LiveStatusData = {
  window_start: number
  window_end: number
  bucket_seconds: number
  short_window_minutes: number
  groups: LiveGroupStatus[]
}
