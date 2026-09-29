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
 * Browser page translation (Chrome/Edge built-in translate, translation
 * extensions) replaces text nodes rendered by React with its own <font>
 * wrappers. React still holds references to the original nodes, so the next
 * update calls removeChild/insertBefore with a node that is no longer a child
 * and the browser throws NotFoundError. That error crashes the route and shows
 * the generic error page.
 *
 * This guard makes those two calls tolerant of detached nodes instead of
 * throwing. See https://github.com/facebook/react/issues/11538.
 *
 * Returns a function that restores the native methods.
 */
export function installTranslationDomGuard(): () => void {
  if (typeof Node !== 'function' || !Node.prototype) {
    return () => undefined
  }

  const nativeRemoveChild = Node.prototype.removeChild
  const nativeInsertBefore = Node.prototype.insertBefore

  Node.prototype.removeChild = function <T extends Node>(
    this: Node,
    child: T
  ): T {
    if (child.parentNode !== this) {
      // Already moved or wrapped by the translator; nothing left to remove here
      if (import.meta.env.DEV) {
        // eslint-disable-next-line no-console
        console.warn('Skipped removeChild on a node with a different parent')
      }
      return child
    }
    return nativeRemoveChild.call(this, child) as T
  }

  Node.prototype.insertBefore = function <T extends Node>(
    this: Node,
    newNode: T,
    referenceNode: Node | null
  ): T {
    if (referenceNode && referenceNode.parentNode !== this) {
      // Reference node was replaced by the translator; append instead of crashing
      if (import.meta.env.DEV) {
        // eslint-disable-next-line no-console
        console.warn('Reference node for insertBefore has a different parent')
      }
      return nativeInsertBefore.call(this, newNode, null) as T
    }
    return nativeInsertBefore.call(this, newNode, referenceNode) as T
  }

  return () => {
    Node.prototype.removeChild = nativeRemoveChild
    Node.prototype.insertBefore = nativeInsertBefore
  }
}
