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
import { getFreshAuthHeaders } from '@/lib/api'
import { createServerError } from '@/lib/server-error-message'

/**
 * A CSV line with every cell quoted. Text a spreadsheet would run as a
 * formula gets a leading apostrophe, since exports carry user-entered names.
 */
export function toCsvLine(cells: string[]): string {
  const quoted = cells.map((cell) => {
    const text =
      /^[=+\-@\t\r]/.test(cell) && !/^-?\d+(\.\d+)?$/.test(cell)
        ? `'${cell}`
        : cell
    return `"${text.replaceAll('"', '""')}"`
  })
  return `${quoted.join(',')}\r\n`
}

/** Save CSV lines as a UTF-8 file that spreadsheet apps open correctly. */
export function downloadCsv(filename: string, lines: string[]): void {
  const blob = new Blob(['\uFEFF', ...lines], {
    type: 'text/csv;charset=utf-8',
  })
  const url = URL.createObjectURL(blob)
  const anchor = document.createElement('a')
  anchor.href = url
  anchor.download = filename
  anchor.click()
  URL.revokeObjectURL(url)
}

/**
 * Read an export stream: the server sends one JSON record per line and ends
 * with an API response object; a stream that ends without a successful one is
 * a failed export. Resolves with the record count the server reports.
 */
export async function readExportStream<T>(
  url: string,
  options: { signal: AbortSignal; onRecord: (record: T) => void }
): Promise<number> {
  const response = await fetch(url, {
    credentials: 'include',
    headers: await getFreshAuthHeaders(),
    signal: options.signal,
  })
  if (!response.ok || !response.body) {
    const body: unknown = await response.json().catch(() => null)
    throw createServerError(body ?? { status: response.status })
  }

  const reader = response.body.getReader()
  const decoder = new TextDecoder()
  let buffer = ''
  let result: { success?: unknown; data?: { count?: number } } | null = null
  for (;;) {
    const { done, value } = await reader.read()
    buffer += done ? decoder.decode() : decoder.decode(value, { stream: true })
    const lines = buffer.split('\n')
    buffer = done ? '' : (lines.pop() ?? '')
    for (const line of lines) {
      if (!line.trim()) continue
      let parsed
      try {
        parsed = JSON.parse(line)
      } catch {
        throw createServerError(null)
      }
      if (typeof parsed.success === 'boolean') result = parsed
      else options.onRecord(parsed as T)
    }
    if (done) break
  }
  if (result?.success !== true) {
    throw createServerError(result)
  }
  return result.data?.count ?? 0
}
