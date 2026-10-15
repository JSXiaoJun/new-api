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

/**
 * Latency health tiers for usage logs.
 *
 * First token: under 10s is good, 10-30s warn, 30-60s slow, 60s+ critical.
 * Total duration: whole streamed responses run long by nature, so the bounds
 * widen to 1min / 3min / 5min.
 */
export type LatencySeverity = 'good' | 'warn' | 'slow' | 'critical'

interface LatencyThresholds {
  warn: number
  slow: number
  critical: number
}

const FIRST_TOKEN_THRESHOLDS_SEC: LatencyThresholds = {
  warn: 10,
  slow: 30,
  critical: 60,
}

const DURATION_THRESHOLDS_SEC: LatencyThresholds = {
  warn: 60,
  slow: 180,
  critical: 300,
}

function classifyLatency(
  seconds: number,
  thresholds: LatencyThresholds
): LatencySeverity {
  if (seconds >= thresholds.critical) return 'critical'
  if (seconds >= thresholds.slow) return 'slow'
  if (seconds >= thresholds.warn) return 'warn'
  return 'good'
}

export function firstTokenSeverity(seconds: number): LatencySeverity {
  return classifyLatency(seconds, FIRST_TOKEN_THRESHOLDS_SEC)
}

export function durationSeverity(seconds: number): LatencySeverity {
  return classifyLatency(seconds, DURATION_THRESHOLDS_SEC)
}

export const latencyTextClasses: Record<LatencySeverity, string> = {
  good: 'text-emerald-600 dark:text-emerald-400',
  warn: 'text-amber-600 dark:text-amber-400',
  slow: 'text-orange-600 dark:text-orange-400',
  critical: 'text-red-600 dark:text-red-400',
}

export const latencyBarClasses: Record<LatencySeverity, string> = {
  good: 'bg-emerald-500',
  warn: 'bg-amber-400',
  slow: 'bg-orange-500',
  critical: 'bg-red-500',
}
