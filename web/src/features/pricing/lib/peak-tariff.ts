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
import type { PeakTariff } from '@/features/system-settings/models/peak-pricing'

import type { PricingModel } from '../types'

export function modelForPeakTariff(
  model: PricingModel,
  tariff: PeakTariff
): PricingModel {
  const input = tariff.input_price ?? 0
  const audio = tariff.audio_price ?? input
  const token = tariff.mode === 'per_token'
  return {
    ...model,
    peak_pricing: undefined,
    billing_mode: tariff.mode,
    billing_expr: tariff.expression,
    quota_type: token || tariff.mode === 'tiered_expr' ? 0 : 1,
    model_price: tariff.price ?? 0,
    model_ratio: input / 2,
    completion_ratio: input > 0 ? (tariff.output_price ?? input) / input : 0,
    cache_ratio: input > 0 ? (tariff.cache_price ?? input) / input : 0,
    create_cache_ratio:
      input > 0 ? (tariff.create_cache_price ?? input) / input : 0,
    image_ratio: input > 0 ? (tariff.image_price ?? input) / input : 0,
    audio_ratio: input > 0 ? audio / input : 0,
    audio_completion_ratio:
      audio > 0 ? (tariff.audio_output_price ?? audio) / audio : 0,
  }
}
