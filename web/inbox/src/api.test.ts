import { afterEach, describe, expect, it, vi } from 'vitest'
import {
  ApiError,
  changeMyPassword,
  createUser,
  deleteUser,
  fetchAuthConfig,
  fetchMe,
  getConversation,
  listConversations,
  listUsers,
  loginWithPassword,
  logout,
  onUnauthorized,
  replyToConversation,
  resetUserPassword,
  setArchived,
  setUserAdmin,
  setupAccount,
} from './api'

type FetchCall = { url: string; init?: RequestInit }

function mockFetch(status: number, body: unknown | string, calls: FetchCall[] = []) {
  const text = typeof body === 'string' ? body : JSON.stringify(body)
  const fn = vi.fn(async (url: string, init?: RequestInit) => {
    calls.push({ url, init })
    return {
      ok: status >= 200 && status < 300,
      status,
      text: async () => text,
    } as Response
  })
  vi.stubGlobal('fetch', fn)
  return calls
}

afterEach(() => {
  vi.unstubAllGlobals()
  onUnauthorized(null)
})

describe('fetchMe', () => {
  it('returns the signed-in operator', async () => {
    mockFetch(200, { email: 'ops@example.com', is_admin: true })
    await expect(fetchMe()).resolves.toEqual({ email: 'ops@example.com', is_admin: true })
  })

  it('returns null when there is no session', async () => {
    mockFetch(401, { error: 'sign in to continue' })
    await expect(fetchMe()).resolves.toBeNull()
  })

  it('propagates other failures', async () => {
    mockFetch(500, { error: 'boom' })
    await expect(fetchMe()).rejects.toBeInstanceOf(ApiError)
  })
})

describe('401 handling', () => {
  it('notifies the registered handler so the app can show the login screen', async () => {
    mockFetch(401, { error: 'sign in to continue' })
    const handler = vi.fn()
    onUnauthorized(handler)

    await expect(listConversations('open')).rejects.toMatchObject({ status: 401 })
    expect(handler).toHaveBeenCalledOnce()
  })

  it('leaves other statuses alone', async () => {
    mockFetch(404, { error: 'conversation not found' })
    const handler = vi.fn()
    onUnauthorized(handler)

    await expect(getConversation(7)).rejects.toMatchObject({ status: 404, notFound: true })
    expect(handler).not.toHaveBeenCalled()
  })
})

describe('listConversations', () => {
  it('requests the given status and unwraps the payload', async () => {
    const calls = mockFetch(200, { conversations: [{ id: 1 }] })
    await expect(listConversations('archived')).resolves.toEqual([{ id: 1 }])
    expect(calls[0]!.url).toBe('/api/inbox/conversations?status=archived')
  })

  it('tolerates a missing list', async () => {
    mockFetch(200, {})
    await expect(listConversations('open')).resolves.toEqual([])
  })
})

describe('replyToConversation', () => {
  it('posts a trimmed message', async () => {
    const calls = mockFetch(201, { id: 9, sender: 'operator', body: 'hi', created_at: '' })
    await replyToConversation(4, '  hi  ')

    expect(calls[0]!.url).toBe('/api/inbox/conversations/4/reply')
    expect(calls[0]!.init?.method).toBe('POST')
    expect(JSON.parse(String(calls[0]!.init?.body))).toEqual({ message: 'hi' })
  })

  it('surfaces the server message on rejection', async () => {
    mockFetch(400, { error: 'message is required' })
    await expect(replyToConversation(4, ' ')).rejects.toThrow('message is required')
  })
})

describe('setArchived', () => {
  it.each([
    [true, '/api/inbox/conversations/3/archive'],
    [false, '/api/inbox/conversations/3/unarchive'],
  ])('archived=%s hits %s', async (archived, url) => {
    const calls = mockFetch(200, { id: 3, status: archived ? 'archived' : 'open' })
    await setArchived(3, archived)
    expect(calls[0]!.url).toBe(url)
  })
})

describe('logout', () => {
  it('posts and accepts an empty body', async () => {
    const calls = mockFetch(204, '')
    await expect(logout()).resolves.toBeUndefined()
    expect(calls[0]).toMatchObject({ url: '/auth/logout' })
    expect(calls[0]!.init?.method).toBe('POST')
  })
})

describe('transport failures', () => {
  it('reports an unreachable server', async () => {
    vi.stubGlobal(
      'fetch',
      vi.fn(async () => {
        throw new TypeError('network down')
      }),
    )
    await expect(listConversations('open')).rejects.toMatchObject({ status: 0 })
  })

  it('falls back to a generic message when the body is not JSON', async () => {
    mockFetch(500, '<html>oops</html>')
    await expect(listConversations('open')).rejects.toThrow(/Something went wrong/)
  })
})

describe('auth config', () => {
  it('reports whether setup is still pending', async () => {
    const calls = mockFetch(200, { setup_required: true })
    await expect(fetchAuthConfig()).resolves.toEqual({ setup_required: true })
    expect(calls[0]!.url).toBe('/api/auth/config')
  })
})

describe('password auth', () => {
  it('logs in with trimmed email and untouched password', async () => {
    const calls = mockFetch(200, { email: 'ops@example.com', is_admin: false })
    await expect(loginWithPassword(' Ops@Example.com ', ' pw ')).resolves.toEqual({
      email: 'ops@example.com',
      is_admin: false,
    })
    expect(calls[0]).toMatchObject({ url: '/api/auth/login' })
    expect(JSON.parse(calls[0]!.init?.body as string)).toEqual({
      email: 'Ops@Example.com',
      password: ' pw ',
    })
  })

  it('surfaces the server message on a rejected login', async () => {
    mockFetch(401, { error: 'incorrect email or password' })
    await expect(loginWithPassword('a@example.com', 'nope')).rejects.toThrow(
      'incorrect email or password',
    )
  })

  it('creates the first account through setup', async () => {
    const calls = mockFetch(201, { email: 'first@example.com', is_admin: true })
    await expect(setupAccount('first@example.com', 'Password1')).resolves.toMatchObject({
      is_admin: true,
    })
    expect(calls[0]).toMatchObject({ url: '/api/auth/setup' })
  })

  it('changes the own password with both secrets', async () => {
    const calls = mockFetch(204, '')
    await expect(changeMyPassword('Old1pass', 'New1pass')).resolves.toBeUndefined()
    expect(calls[0]).toMatchObject({ url: '/api/me/password' })
    expect(JSON.parse(calls[0]!.init?.body as string)).toEqual({
      current_password: 'Old1pass',
      new_password: 'New1pass',
    })
  })
})

describe('user management', () => {
  it('lists users and unwraps the payload', async () => {
    mockFetch(200, { users: [{ id: 1, email: 'a@example.com', is_admin: true }] })
    await expect(listUsers()).resolves.toEqual([{ id: 1, email: 'a@example.com', is_admin: true }])
  })

  it('creates a user with the admin flag', async () => {
    const calls = mockFetch(201, { id: 2, email: 'b@example.com', is_admin: false })
    await createUser(' B@example.com ', 'Password1', false)
    expect(calls[0]).toMatchObject({ url: '/api/users' })
    expect(JSON.parse(calls[0]!.init?.body as string)).toEqual({
      email: 'B@example.com',
      password: 'Password1',
      is_admin: false,
    })
  })

  it('deletes, promotes and resets against the id routes', async () => {
    let calls = mockFetch(204, '')
    await deleteUser(7)
    expect(calls[0]).toMatchObject({ url: '/api/users/7' })
    expect(calls[0]!.init?.method).toBe('DELETE')

    calls = mockFetch(200, { id: 7, email: 'a@example.com', is_admin: true })
    await setUserAdmin(7, true)
    expect(calls[0]).toMatchObject({ url: '/api/users/7/admin' })

    calls = mockFetch(204, '')
    await resetUserPassword(7, 'NewPass1')
    expect(calls[0]).toMatchObject({ url: '/api/users/7/password' })
  })
})
