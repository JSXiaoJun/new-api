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
import { render, screen, waitFor } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { beforeEach, describe, expect, it, vi } from 'vitest'

import { api } from '@/lib/api'
import { STATUS_QUERY_KEY } from '@/lib/status-query'

import { UserAuthForm } from '../components/user-auth-form'

let issuedCaptchas = 0

async function renderSignIn(status: Record<string, unknown>) {
  const client = new QueryClient({
    defaultOptions: { queries: { retry: false }, mutations: { retry: false } },
  })
  client.setQueryData(STATUS_QUERY_KEY, status)
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
}

async function fillCredentials(user: ReturnType<typeof userEvent.setup>) {
  await user.type(
    screen.getByRole('textbox', { name: 'Username or Email' }),
    'alice'
  )
  await user.type(screen.getByLabelText('Password'), 'correct-horse')
}

beforeEach(() => {
  issuedCaptchas = 0
  vi.spyOn(api, 'get').mockImplementation(async (url: string) => {
    if (url !== '/api/captcha') throw new Error(`unexpected GET ${url}`)
    issuedCaptchas += 1
    return {
      data: {
        success: true,
        message: '',
        data: {
          captcha_id: `captcha-${issuedCaptchas}`,
          image: `data:image/png;base64,image-${issuedCaptchas}`,
        },
      },
    } as never
  })
})

describe('Sign-in simple captcha', () => {
  it('does not show the captcha or request one when the switch is off', async () => {
    await renderSignIn({ simple_captcha_check: false })

    expect(
      screen.getByRole('textbox', { name: 'Username or Email' })
    ).toBeInTheDocument()
    expect(screen.queryByRole('textbox', { name: 'Captcha' })).toBeNull()
    expect(api.get).not.toHaveBeenCalled()
  })

  it('blocks the login request when no captcha answer is entered', async () => {
    const post = vi.spyOn(api, 'post')
    const user = userEvent.setup()
    await renderSignIn({ simple_captcha_check: true })
    await screen.findByRole('img', { name: 'Captcha image' })

    await fillCredentials(user)
    await user.click(screen.getByRole('button', { name: 'Sign in' }))

    expect(post).not.toHaveBeenCalled()
  })

  it('sends the answer with the login request and then loads a new captcha', async () => {
    const post = vi.spyOn(api, 'post').mockResolvedValue({
      data: {
        success: false,
        message: 'Captcha is incorrect or expired, please try again',
      },
    } as never)
    const user = userEvent.setup()
    await renderSignIn({ simple_captcha_check: true })
    await screen.findByRole('img', { name: 'Captcha image' })

    await fillCredentials(user)
    await user.type(screen.getByRole('textbox', { name: 'Captcha' }), 'ab3d ')
    await user.click(screen.getByRole('button', { name: 'Sign in' }))

    await waitFor(() => expect(post).toHaveBeenCalledTimes(1))
    expect(post.mock.calls[0][2]).toMatchObject({
      params: { captcha_id: 'captcha-1', captcha_code: 'ab3d' },
    })
    await waitFor(() =>
      expect(
        screen.getByRole('img', { name: 'Captcha image' })
      ).toHaveAttribute('src', 'data:image/png;base64,image-2')
    )
    expect(screen.getByRole('textbox', { name: 'Captcha' })).toHaveValue('')
  })
})
