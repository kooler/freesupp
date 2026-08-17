import { afterEach, describe, expect, it, vi } from 'vitest'
import { flushPromises, mount } from '@vue/test-utils'
import App from './App.vue'
import { onUnauthorized } from './api'

function stubFetch(routes: Record<string, { status: number; body: unknown }>) {
  vi.stubGlobal(
    'fetch',
    vi.fn(async (url: string) => {
      const path = url.split('?')[0]!
      const route = routes[path] ?? { status: 404, body: { error: 'not found' } }
      return {
        ok: route.status >= 200 && route.status < 300,
        status: route.status,
        text: async () => JSON.stringify(route.body),
      } as Response
    }),
  )
}

const authConfig = { status: 200, body: { setup_required: false } }
const me = { status: 200, body: { email: 'ops@example.com', is_admin: true } }
const noSession = { status: 401, body: { error: 'sign in to continue' } }

afterEach(() => {
  vi.unstubAllGlobals()
  onUnauthorized(null)
})

describe('App', () => {
  it('shows the login screen when there is no session', async () => {
    stubFetch({ '/api/me': noSession, '/api/auth/config': authConfig })

    const wrapper = mount(App)
    await flushPromises()

    expect(wrapper.find('[data-testid="password-login"]').exists()).toBe(true)
  })

  it('guides through account creation on a fresh install', async () => {
    stubFetch({
      '/api/me': noSession,
      '/api/auth/config': { status: 200, body: { setup_required: true } },
    })

    const wrapper = mount(App)
    await flushPromises()

    expect(wrapper.find('[data-testid="setup-email"]').exists()).toBe(true)
    expect(wrapper.find('[data-testid="password-login"]').exists()).toBe(false)
  })

  it('shows the inbox for a signed-in operator', async () => {
    stubFetch({
      '/api/me': me,
      '/api/auth/config': authConfig,
      '/api/inbox/conversations': { status: 200, body: { conversations: [] } },
    })

    const wrapper = mount(App)
    await flushPromises()

    expect(wrapper.text()).toContain('ops@example.com')
    expect(wrapper.find('[data-testid="conversation-list"]').exists()).toBe(true)
    wrapper.unmount()
  })

  it('opens and closes the settings screen', async () => {
    stubFetch({
      '/api/me': me,
      '/api/auth/config': authConfig,
      '/api/inbox/conversations': { status: 200, body: { conversations: [] } },
      '/api/users': { status: 200, body: { users: [] } },
    })

    const wrapper = mount(App)
    await flushPromises()

    await wrapper.get('[data-testid="open-settings"]').trigger('click')
    await flushPromises()
    expect(wrapper.find('[data-testid="change-password"]').exists()).toBe(true)

    await wrapper.get('[data-testid="close-settings"]').trigger('click')
    await flushPromises()
    expect(wrapper.find('[data-testid="conversation-list"]').exists()).toBe(true)
    wrapper.unmount()
  })

  it('falls back to the login screen when a later request 401s', async () => {
    stubFetch({
      '/api/me': me,
      '/api/auth/config': authConfig,
      '/api/inbox/conversations': noSession,
    })

    const wrapper = mount(App)
    await flushPromises()

    expect(wrapper.find('[data-testid="password-login"]').exists()).toBe(true)
    expect(wrapper.text()).toContain('Your session expired')
  })

  it('returns to the login screen after signing out', async () => {
    stubFetch({
      '/api/me': me,
      '/api/auth/config': authConfig,
      '/api/inbox/conversations': { status: 200, body: { conversations: [] } },
      '/auth/logout': { status: 204, body: null },
    })

    const wrapper = mount(App)
    await flushPromises()

    await wrapper.get('[data-testid="logout"]').trigger('click')
    await flushPromises()

    expect(wrapper.find('[data-testid="password-login"]').exists()).toBe(true)
    expect(wrapper.text()).not.toContain('Your session expired')
  })
})
