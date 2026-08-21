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
import { assert, describe, test } from 'vitest'

import { parseHeaderNavModules } from '@/lib/nav-modules'

import { buildTopNavLinks } from '../use-top-nav-links'

describe('top navigation links', () => {
  test('places a configured infinite canvas link immediately after gallery', () => {
    const links = buildTopNavLinks({
      t: (key) => key,
      modules: parseHeaderNavModules(undefined),
      docsLink: 'https://docs.example.com',
      galleryLink: 'https://gallery.example.com',
      infiniteCanvasLink: 'https://canvas.example.com',
      isAuthed: false,
      isAdmin: false,
    })

    const galleryIndex = links.findIndex((link) => link.title === 'Gallery')
    assert.notEqual(galleryIndex, -1)
    assert.deepEqual(links[galleryIndex + 1], {
      title: 'Infinite Canvas',
      href: 'https://canvas.example.com',
      external: true,
    })
  })

  test('omits infinite canvas when no link is configured', () => {
    const links = buildTopNavLinks({
      t: (key) => key,
      modules: parseHeaderNavModules(undefined),
      galleryLink: 'https://gallery.example.com',
      infiniteCanvasLink: '',
      isAuthed: false,
      isAdmin: false,
    })

    assert.equal(
      links.some((link) => link.title === 'Infinite Canvas'),
      false
    )
  })
})
