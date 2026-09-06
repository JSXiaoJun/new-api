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

export type FirstTokenRuleComparison = 'gt' | 'gte'
export type FirstTokenRuleOperation = 'add' | 'subtract' | 'multiply'

export type FirstTokenDisplayRule = {
  id: string
  comparison: FirstTokenRuleComparison
  threshold: number
  operation: FirstTokenRuleOperation
  value: number
}

export type FirstTokenDisplayConfig = {
  enabled: boolean
  rules: FirstTokenDisplayRule[]
}

export const FIRST_TOKEN_DISPLAY_OPTION_KEY = 'FirstTokenDisplayRules'
export const MAX_FIRST_TOKEN_DISPLAY_RULES = 50
export const MAX_FIRST_TOKEN_DISPLAY_VALUE = 86400

export const DEFAULT_FIRST_TOKEN_DISPLAY_CONFIG: FirstTokenDisplayConfig = {
  enabled: false,
  rules: [
    {
      id: 'over-9-half',
      comparison: 'gt',
      threshold: 9,
      operation: 'multiply',
      value: 0.5,
    },
    {
      id: 'from-5-subtract-4',
      comparison: 'gte',
      threshold: 5,
      operation: 'subtract',
      value: 4,
    },
    {
      id: 'from-3-subtract-2',
      comparison: 'gte',
      threshold: 3,
      operation: 'subtract',
      value: 2,
    },
  ],
}

function cloneDefaultConfig(): FirstTokenDisplayConfig {
  return {
    enabled: DEFAULT_FIRST_TOKEN_DISPLAY_CONFIG.enabled,
    rules: DEFAULT_FIRST_TOKEN_DISPLAY_CONFIG.rules.map((rule) => ({
      ...rule,
    })),
  }
}

export function isValidFirstTokenDisplayConfig(
  value: unknown
): value is FirstTokenDisplayConfig {
  if (!value || typeof value !== 'object') return false
  const config = value as Record<string, unknown>
  if (typeof config.enabled !== 'boolean' || !Array.isArray(config.rules)) {
    return false
  }
  if (config.rules.length > MAX_FIRST_TOKEN_DISPLAY_RULES) return false

  const ids = new Set<string>()
  const conditions = new Set<string>()
  return config.rules.every((candidate) => {
    if (!candidate || typeof candidate !== 'object') return false
    const rule = candidate as Record<string, unknown>
    if (
      typeof rule.id !== 'string' ||
      rule.id.trim() === '' ||
      rule.id.length > 100 ||
      ids.has(rule.id)
    ) {
      return false
    }
    ids.add(rule.id)

    if (rule.comparison !== 'gt' && rule.comparison !== 'gte') return false
    if (
      typeof rule.threshold !== 'number' ||
      !Number.isFinite(rule.threshold) ||
      rule.threshold < 0 ||
      rule.threshold > MAX_FIRST_TOKEN_DISPLAY_VALUE
    ) {
      return false
    }
    const condition = `${rule.comparison}:${rule.threshold}`
    if (conditions.has(condition)) return false
    conditions.add(condition)

    if (
      rule.operation !== 'add' &&
      rule.operation !== 'subtract' &&
      rule.operation !== 'multiply'
    ) {
      return false
    }
    return (
      typeof rule.value === 'number' &&
      Number.isFinite(rule.value) &&
      rule.value >= 0 &&
      rule.value <= MAX_FIRST_TOKEN_DISPLAY_VALUE
    )
  })
}

export function parseFirstTokenDisplayConfig(
  raw: unknown
): FirstTokenDisplayConfig {
  try {
    const parsed = typeof raw === 'string' ? JSON.parse(raw) : raw
    if (!isValidFirstTokenDisplayConfig(parsed)) return cloneDefaultConfig()
    return {
      enabled: parsed.enabled,
      rules: parsed.rules.map((rule) => ({ ...rule })),
    }
  } catch {
    return cloneDefaultConfig()
  }
}

export function adjustFirstTokenDisplaySeconds(
  seconds: number,
  config: FirstTokenDisplayConfig = DEFAULT_FIRST_TOKEN_DISPLAY_CONFIG
): number | null {
  if (!Number.isFinite(seconds) || seconds <= 0) return null
  if (!config.enabled) return seconds

  const rule = config.rules.find((candidate) =>
    candidate.comparison === 'gt'
      ? seconds > candidate.threshold
      : seconds >= candidate.threshold
  )
  if (!rule) return seconds

  let adjusted = seconds
  if (rule.operation === 'add') {
    adjusted += rule.value
  } else if (rule.operation === 'subtract') {
    adjusted -= rule.value
  } else {
    adjusted *= rule.value
  }

  return Number.isFinite(adjusted) ? Math.max(0, adjusted) : seconds
}
