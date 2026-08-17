import { afterEach, describe, expect, it, vi } from 'vitest'
import { flushPromises, mount } from '@vue/test-utils'
import SettingsScreen from './SettingsScreen.vue'

type Call = { url: string; method: string }

function stubFetch(calls: Call[]) {
  vi.stubGlobal(
    'fetch',
    vi.fn(async (url: string, init?: RequestInit) => {
      calls.push({ url, method: init?.method ?? 'GET' })
      const body =
        url === '/api/users' && !init?.method
          ? { users: [{ id: 7, email: 'bob@example.com', is_admin: false, created_at: '' }] }
          : null
      return {
        ok: true,
        status: body ? 200 : 204,
        text: async () => (body ? JSON.stringify(body) : ''),
      } as Response
    }),
  )
}

function mountAsAdmin() {
  return mount(SettingsScreen, { props: { me: { email: 'admin@example.com', is_admin: true } } })
}

afterEach(() => vi.unstubAllGlobals())

describe('SettingsScreen delete confirmation', () => {
  it('asks for confirmation in the row instead of deleting straight away', async () => {
    const calls: Call[] = []
    stubFetch(calls)

    const wrapper = mountAsAdmin()
    await flushPromises()

    await wrapper.get('[data-testid="delete-user-7"]').trigger('click')
    expect(wrapper.text()).toContain('Delete bob@example.com?')
    // Nothing is deleted until the confirmation is clicked.
    expect(calls.some((c) => c.method === 'DELETE')).toBe(false)

    await wrapper.get('[data-testid="confirm-delete-7"]').trigger('click')
    await flushPromises()

    expect(calls).toContainEqual({ url: '/api/users/7', method: 'DELETE' })
    expect(wrapper.text()).not.toContain('bob@example.com')
  })

  it('cancelling leaves the user in place', async () => {
    const calls: Call[] = []
    stubFetch(calls)

    const wrapper = mountAsAdmin()
    await flushPromises()

    await wrapper.get('[data-testid="delete-user-7"]').trigger('click')
    const cancel = wrapper.findAll('button').find((b) => b.text() === 'Cancel')!
    await cancel.trigger('click')

    expect(wrapper.text()).not.toContain('Delete bob@example.com?')
    expect(wrapper.text()).toContain('bob@example.com')
    expect(calls.some((c) => c.method === 'DELETE')).toBe(false)
  })
})
