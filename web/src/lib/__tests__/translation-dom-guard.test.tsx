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
import { act } from 'react'
import { createRoot, type Root } from 'react-dom/client'
import { afterEach, beforeEach, describe, expect, it } from 'vitest'

import { installTranslationDomGuard } from '../translation-dom-guard'

// Mimics Chrome/Edge page translation: each text node is replaced by
// <font><font>translated</font></font>, so React's text node reference
// is detached from the parent React thinks it lives in.
function translateTextNodes(container: HTMLElement) {
  const walker = document.createTreeWalker(container, NodeFilter.SHOW_TEXT)
  const textNodes: Text[] = []
  while (walker.nextNode()) textNodes.push(walker.currentNode as Text)
  for (const node of textNodes) {
    const outer = document.createElement('font')
    const inner = document.createElement('font')
    inner.textContent = `translated ${node.data}`
    outer.appendChild(inner)
    node.parentNode?.replaceChild(outer, node)
  }
}

function Page(props: { step: number }) {
  // Switching pages: a text sibling disappears, and an element is inserted
  // before a text node that the translator has already replaced.
  return (
    <div>
      {props.step === 0 && 'Wallet'}
      {props.step === 1 && <span>Profile</span>}
      {'Balance'}
    </div>
  )
}

describe('translation DOM guard', () => {
  let container: HTMLDivElement
  let root: Root
  let errors: unknown[]
  let uninstall: () => void

  beforeEach(() => {
    container = document.createElement('div')
    document.body.appendChild(container)
    errors = []
    root = createRoot(container, {
      onUncaughtError: (error) => errors.push(error),
      onCaughtError: (error) => errors.push(error),
    })
    uninstall = installTranslationDomGuard()
  })

  afterEach(() => {
    act(() => root.unmount())
    container.remove()
    uninstall()
  })

  it('keeps React updating after page translation replaces rendered text nodes', () => {
    act(() => root.render(<Page step={0} />))
    translateTextNodes(container)

    act(() => root.render(<Page step={1} />))

    expect(errors).toEqual([])
    expect(container.querySelector('span')?.textContent).toBe('Profile')
  })

  it('restores native DOM methods when uninstalled', () => {
    uninstall()

    const parent = document.createElement('div')
    const orphan = document.createElement('span')

    expect(() => parent.removeChild(orphan)).toThrow()
    expect(() =>
      parent.insertBefore(document.createElement('i'), orphan)
    ).toThrow()
  })
})
