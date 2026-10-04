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

/** Mirrors operation_setting.MaxTopUpBonusPercent on the server. */
export const MAX_TOP_UP_BONUS_PERCENT = 1000

export type AmountBonusTier = {
  amount: number
  bonusPercent: number
}

export function isValidAmountBonusTier(tier: AmountBonusTier): boolean {
  return (
    Number.isSafeInteger(tier.amount) &&
    tier.amount > 0 &&
    Number.isFinite(tier.bonusPercent) &&
    tier.bonusPercent > 0 &&
    tier.bonusPercent <= MAX_TOP_UP_BONUS_PERCENT
  )
}

/**
 * Parses the stored `{"threshold": percent}` JSON into tiers sorted by
 * threshold. Returns null when the value is not a JSON object.
 */
export function parseAmountBonusTiers(value: string): AmountBonusTier[] | null {
  let parsed: unknown
  try {
    parsed = JSON.parse(value.trim() || '{}')
  } catch {
    return null
  }
  if (!parsed || typeof parsed !== 'object' || Array.isArray(parsed)) {
    return null
  }
  // Only accept the shapes the server's map[int]float64 decoder accepts:
  // plain decimal integer keys and JSON number values. Anything else maps to
  // NaN so validation rejects it instead of the server failing the save.
  return Object.entries(parsed as Record<string, unknown>)
    .map(([amount, percent]) => ({
      amount: /^\d+$/.test(amount) ? Number(amount) : Number.NaN,
      bonusPercent: typeof percent === 'number' ? percent : Number.NaN,
    }))
    .sort((a, b) => a.amount - b.amount)
}

export function serializeAmountBonusTiers(tiers: AmountBonusTier[]): string {
  const result: Record<string, number> = {}
  for (const tier of [...tiers].sort((a, b) => a.amount - b.amount)) {
    result[String(tier.amount)] = tier.bonusPercent
  }
  return JSON.stringify(result, null, 2)
}

/**
 * Returns a validation error key for the stored JSON, or null when every tier
 * can be accepted by the server.
 */
export function getAmountBonusError(value: string): string | null {
  const tiers = parseAmountBonusTiers(value)
  if (tiers === null) {
    return 'Top-up bonus must be a JSON object'
  }
  if (!tiers.every(isValidAmountBonusTier)) {
    return 'Each bonus tier needs a whole amount above 0 and a percentage between 0 and 1000'
  }
  return null
}
