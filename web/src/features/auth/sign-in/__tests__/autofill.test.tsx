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
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import {
  createMemoryHistory,
  createRootRoute,
  createRouter,
  RouterProvider,
} from '@tanstack/react-router'
import { render, screen } from '@testing-library/react'
import { expect, test } from 'vitest'

import { STATUS_QUERY_KEY } from '@/lib/status-query'

import { UserAuthForm } from '../components/user-auth-form'

test('the sign-in form lets browsers fill the saved username and password', async () => {
  const client = new QueryClient()
  client.setQueryData(STATUS_QUERY_KEY, { simple_captcha_check: false })
  const root = createRootRoute({
    component: () => (
      <QueryClientProvider client={client}>
        <UserAuthForm />
      </QueryClientProvider>
    ),
  })
  const router = createRouter({
    routeTree: root,
    history: createMemoryHistory({ initialEntries: ['/'] }),
  })
  await router.load()
  render(<RouterProvider router={router} />)

  expect(
    screen.getByRole('textbox', { name: 'Username or Email' })
  ).toHaveAttribute('autocomplete', 'username')
  expect(screen.getByLabelText('Password')).toHaveAttribute(
    'autocomplete',
    'current-password'
  )
})
