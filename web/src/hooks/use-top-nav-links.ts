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
import { useMemo } from 'react'
import { useTranslation } from 'react-i18next'

import { useStatus } from '@/hooks/use-status'
import {
  parseHeaderNavModulesFromStatus,
  type HeaderNavModules,
} from '@/lib/nav-modules'
import { ROLE } from '@/lib/roles'
import { useAuthStore } from '@/stores/auth-store'

export type TopNavLink = {
  title: string
  href: string
  disabled?: boolean
  requiresAuth?: boolean
  external?: boolean
}

type BuildTopNavLinksOptions = {
  t: (key: string) => string
  modules: HeaderNavModules
  docsLink?: string
  galleryLink: string
  infiniteCanvasLink: string
  isAuthed: boolean
  isAdmin: boolean
}

export function buildTopNavLinks(
  options: BuildTopNavLinksOptions
): TopNavLink[] {
  const links: TopNavLink[] = []

  if (options.modules.home !== false) {
    links.push({ title: options.t('Home'), href: '/' })
  }

  if (options.modules.console !== false) {
    links.push({ title: options.t('Console'), href: '/dashboard' })
  }

  const pricing = options.modules.pricing
  if (pricing && typeof pricing === 'object' && pricing.enabled) {
    const requiresAuth = pricing.requireAuth && !options.isAuthed
    links.push({
      title: options.t('Model Square'),
      href: '/pricing',
      requiresAuth,
    })
  }

  const rankings = options.modules.rankings
  if (
    rankings &&
    typeof rankings === 'object' &&
    rankings.enabled &&
    (!rankings.adminOnly || options.isAdmin)
  ) {
    const requiresAuth = rankings.requireAuth && !options.isAuthed
    links.push({
      title: options.t('Rankings'),
      href: '/rankings',
      requiresAuth,
    })
  }

  if (options.modules.docs !== false) {
    if (options.docsLink) {
      links.push({
        title: options.t('Docs'),
        href: options.docsLink,
        external: true,
      })
    } else {
      links.push({ title: options.t('Docs'), href: '/docs' })
    }
  }

  if (options.galleryLink) {
    links.push({
      title: options.t('Gallery'),
      href: options.galleryLink,
      external: true,
    })
  }

  if (options.infiniteCanvasLink) {
    links.push({
      title: options.t('Infinite Canvas'),
      href: options.infiniteCanvasLink,
      external: true,
    })
  }

  if (options.modules.about !== false) {
    links.push({ title: options.t('About'), href: '/about' })
  }

  return links
}

/**
 * Generate top navigation links based on HeaderNavModules configuration from backend /api/status
 * Backend format example (stringified JSON):
 * {
 *   home: true,
 *   console: true,
 *   pricing: { enabled: true, requireAuth: false },
 *   rankings: { enabled: true, requireAuth: false, adminOnly: false },
 *   docs: true,
 *   about: true
 * }
 */
export function useTopNavLinks(): TopNavLink[] {
  const { t } = useTranslation()
  const { status } = useStatus()
  const { auth } = useAuthStore()

  // Parse HeaderNavModules
  const modules = useMemo(() => {
    return parseHeaderNavModulesFromStatus(
      status as Record<string, unknown> | null
    )
  }, [status])

  // Documentation link (may be external)
  const docsLink: string | undefined = status?.docs_link as string | undefined
  const galleryLink = String(status?.gallery_link ?? '').trim()
  const infiniteCanvasLink = String(status?.infinite_canvas_link ?? '').trim()

  const isAuthed = !!auth?.user
  const isAdmin = (auth?.user?.role ?? 0) >= ROLE.ADMIN

  return buildTopNavLinks({
    t,
    modules,
    docsLink,
    galleryLink,
    infiniteCanvasLink,
    isAuthed,
    isAdmin,
  })
}
