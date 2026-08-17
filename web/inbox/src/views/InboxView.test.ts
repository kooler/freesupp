import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { flushPromises, mount } from '@vue/test-utils'
import InboxView from './InboxView.vue'
import type { ConversationDetail, ConversationSummary } from '@/api'

function conv(over: Partial<ConversationSummary> = {}): ConversationSummary {
  return {
    id: 1,
    visitor_email: 'visitor@example.com',
    visitor_name: 'Ada Lovelace',
    status: 'open',
    unread: false,
    snippet: 'my printer is on fire',
    last_sender: 'visitor',
    created_at: '2026-08-14T10:00:00Z',
    last_message_at: '2026-08-14T11:00:00Z',
    ...over,
  }
}

function detail(over: Partial<ConversationDetail> = {}): ConversationDetail {
  return {
    id: 1,
    visitor_email: 'visitor@example.com',
    visitor_name: 'Ada Lovelace',
    token: 'tok',
    status: 'open',
    unread: false,
    created_at: '2026-08-14T10:00:00Z',
    last_message_at: '2026-08-14T11:00:00Z',
    messages: [{ id: 1, sender: 'visitor', body: 'hello', created_at: '2026-08-14T10:00:00Z' }],
    ...over,
  }
}

/** server holds the payloads the stubbed fetch answers with. */
const server = {
  open: [] as ConversationSummary[],
  archived: [] as ConversationSummary[],
  detail: detail(),
}
let calls: string[] = []

beforeEach(() => {
  server.open = [conv()]
  server.archived = []
  server.detail = detail()
  calls = []
  history.replaceState(null, '', '/')

  vi.stubGlobal(
    'fetch',
    vi.fn(async (url: string, init?: RequestInit) => {
      calls.push(`${init?.method ?? 'GET'} ${url}`)
      const [path, query] = url.split('?')
      const json = (status: number, body: unknown) =>
        ({ ok: status < 400, status, text: async () => JSON.stringify(body) }) as Response

      if (path === '/api/inbox/conversations') {
        const list = query === 'status=archived' ? server.archived : server.open
        return json(200, { conversations: list })
      }
      if (/^\/api\/inbox\/conversations\/\d+$/.test(path!)) {
        server.detail.unread = false
        return json(200, server.detail)
      }
      if (path!.endsWith('/reply')) {
        return json(201, { id: 2, sender: 'operator', body: 'hi', created_at: '' })
      }
      if (path!.endsWith('/archive')) {
        server.detail = { ...server.detail, status: 'archived' }
        server.archived = server.open.map((c) => ({ ...c, status: 'archived' as const }))
        server.open = []
        return json(200, server.detail)
      }
      return json(404, { error: 'not found' })
    }),
  )
})

afterEach(() => {
  vi.unstubAllGlobals()
  vi.useRealTimers()
})

function render() {
  return mount(InboxView, {
    props: { me: { email: 'ops@example.com', is_admin: true } },
    attachTo: document.body,
  })
}

function draftValue(wrapper: ReturnType<typeof render>) {
  return wrapper.get<HTMLTextAreaElement>('[data-testid="reply-input"]').element.value
}

describe('InboxView', () => {
  it('loads the open list and shows the operator', async () => {
    const wrapper = render()
    await flushPromises()

    expect(wrapper.text()).toContain('ops@example.com')
    expect(wrapper.find('[data-testid="row-1"]').exists()).toBe(true)
    expect(calls).toContain('GET /api/inbox/conversations?status=open')
    wrapper.unmount()
  })

  it('puts the unread count in the page title', async () => {
    server.open = [conv({ id: 1, unread: true }), conv({ id: 2, unread: true }), conv({ id: 3 })]
    const wrapper = render()
    await flushPromises()

    expect(document.title).toBe('(2) Inbox')
    wrapper.unmount()
  })

  it('opens a conversation, clears its unread mark and deep-links to it', async () => {
    server.open = [conv({ id: 1, unread: true })]
    const wrapper = render()
    await flushPromises()

    await wrapper.get('[data-testid="row-1"]').trigger('click')
    await flushPromises()

    expect(calls).toContain('GET /api/inbox/conversations/1')
    expect(wrapper.get('[data-testid="conversation-detail"]').text()).toContain('hello')
    expect(wrapper.get('[data-testid="row-1"]').attributes('data-unread')).toBe('false')
    expect(document.title).toBe('Inbox')
    expect(location.pathname).toBe('/conversations/1')
    wrapper.unmount()
  })

  it('opens the conversation the URL points at', async () => {
    history.replaceState(null, '', '/conversations/1')
    const wrapper = render()
    await flushPromises()

    expect(calls).toContain('GET /api/inbox/conversations/1')
    expect(wrapper.get('[data-testid="conversation-detail"]').text()).toContain('hello')
    wrapper.unmount()
  })

  it('follows browser back/forward to another conversation', async () => {
    server.open = [conv({ id: 1 }), conv({ id: 2 })]
    const wrapper = render()
    await flushPromises()

    await wrapper.get('[data-testid="row-1"]').trigger('click')
    await flushPromises()
    await wrapper.get('[data-testid="row-2"]').trigger('click')
    await flushPromises()

    history.replaceState(null, '', '/conversations/1')
    window.dispatchEvent(new PopStateEvent('popstate'))
    await flushPromises()

    expect(calls.filter((c) => c === 'GET /api/inbox/conversations/1')).toHaveLength(2)
    wrapper.unmount()
  })

  // popstate is registered on window, so an unmounted view would keep firing
  // requests for every back/forward press.
  it('stops listening for popstate once unmounted', async () => {
    const wrapper = render()
    await flushPromises()
    wrapper.unmount()

    const before = calls.length
    history.replaceState(null, '', '/conversations/1')
    window.dispatchEvent(new PopStateEvent('popstate'))
    await flushPromises()

    expect(calls).toHaveLength(before)
  })

  // The detail pane polls on tab focus only, which never fires for an operator
  // reading the thread — re-clicking the open row is the manual refresh.
  it('refetches the open conversation when its row is clicked again', async () => {
    const wrapper = render()
    await flushPromises()

    await wrapper.get('[data-testid="row-1"]').trigger('click')
    await flushPromises()
    await wrapper.get('[data-testid="row-1"]').trigger('click')
    await flushPromises()

    expect(calls.filter((c) => c === 'GET /api/inbox/conversations/1')).toHaveLength(2)
    wrapper.unmount()
  })

  it('switches to the archived tab', async () => {
    server.archived = [conv({ id: 5, status: 'archived' })]
    const wrapper = render()
    await flushPromises()

    // reka-ui tabs activate on mousedown, not click.
    await wrapper
      .findAll('[role="tab"]')
      .find((t) => t.text() === 'Archived')!
      .trigger('mousedown', { button: 0 })
    await flushPromises()

    expect(calls).toContain('GET /api/inbox/conversations?status=archived')
    expect(wrapper.find('[data-testid="row-5"]').exists()).toBe(true)
    wrapper.unmount()
  })

  it('sends a reply and refreshes both panes', async () => {
    const wrapper = render()
    await flushPromises()
    await wrapper.get('[data-testid="row-1"]').trigger('click')
    await flushPromises()

    await wrapper.get('[data-testid="reply-input"]').setValue('on it')
    await wrapper.get('[data-testid="send-reply"]').trigger('click')
    await flushPromises()

    expect(calls).toContain('POST /api/inbox/conversations/1/reply')
    expect(calls.filter((c) => c === 'GET /api/inbox/conversations?status=open')).toHaveLength(2)
    wrapper.unmount()
  })

  it('archiving drops the row from the open tab and closes the detail', async () => {
    const wrapper = render()
    await flushPromises()
    await wrapper.get('[data-testid="row-1"]').trigger('click')
    await flushPromises()

    await wrapper.findAll('button').find((b) => b.text() === 'Archive')!.trigger('click')
    await flushPromises()

    expect(calls).toContain('POST /api/inbox/conversations/1/archive')
    expect(wrapper.find('[data-testid="row-1"]').exists()).toBe(false)
    expect(wrapper.get('[data-testid="conversation-detail"]').text()).toContain(
      'Select a conversation',
    )
    expect(location.pathname).toBe('/')
    wrapper.unmount()
  })

  it('refetches the list every 20 seconds', async () => {
    vi.useFakeTimers()
    const wrapper = render()
    await vi.advanceTimersByTimeAsync(0)

    const listCalls = () => calls.filter((c) => c.startsWith('GET /api/inbox/conversations?'))
    expect(listCalls()).toHaveLength(1)

    await vi.advanceTimersByTimeAsync(20_000)
    expect(listCalls()).toHaveLength(2)

    await vi.advanceTimersByTimeAsync(20_000)
    expect(listCalls()).toHaveLength(3)

    wrapper.unmount()
    await vi.advanceTimersByTimeAsync(40_000)
    expect(listCalls()).toHaveLength(3)
  })

  it('refreshes the open conversation when the tab regains focus', async () => {
    const wrapper = render()
    await flushPromises()
    await wrapper.get('[data-testid="row-1"]').trigger('click')
    await flushPromises()

    const detailCalls = () => calls.filter((c) => c === 'GET /api/inbox/conversations/1')
    expect(detailCalls()).toHaveLength(1)

    window.dispatchEvent(new Event('focus'))
    await flushPromises()
    expect(detailCalls()).toHaveLength(2)
    wrapper.unmount()
  })

  // The detail pane refetches on every focus; a blip there must not throw away
  // the reply being typed.
  it('keeps the thread and the draft when a focus refresh fails', async () => {
    const wrapper = render()
    await flushPromises()
    await wrapper.get('[data-testid="row-1"]').trigger('click')
    await flushPromises()
    await wrapper.get('[data-testid="reply-input"]').setValue('half-written reply')

    vi.stubGlobal(
      'fetch',
      vi.fn(async () => {
        throw new TypeError('network down')
      }),
    )
    window.dispatchEvent(new Event('focus'))
    await flushPromises()

    expect(wrapper.get('[data-testid="conversation-detail"]').text()).toContain('hello')
    expect(draftValue(wrapper)).toBe('half-written reply')
    wrapper.unmount()
  })

  it('reports a conversation that no longer exists', async () => {
    const wrapper = render()
    await flushPromises()

    vi.stubGlobal(
      'fetch',
      vi.fn(async () => ({
        ok: false,
        status: 404,
        text: async () => JSON.stringify({ error: 'not found' }),
      })),
    )

    await wrapper.get('[data-testid="row-1"]').trigger('click')
    await flushPromises()

    expect(wrapper.get('[data-testid="detail-error"]').text()).toBe(
      'This conversation no longer exists.',
    )
    wrapper.unmount()
  })

  it('reports a failed reply without losing the conversation', async () => {
    const wrapper = render()
    await flushPromises()
    await wrapper.get('[data-testid="row-1"]').trigger('click')
    await flushPromises()

    vi.stubGlobal(
      'fetch',
      vi.fn(async () => ({
        ok: false,
        status: 400,
        text: async () => JSON.stringify({ error: 'message is required' }),
      })),
    )

    await wrapper.get('[data-testid="reply-input"]').setValue('oops')
    await wrapper.get('[data-testid="send-reply"]').trigger('click')
    await flushPromises()

    expect(wrapper.get('[data-testid="detail-error"]').text()).toBe('message is required')
    // The operator must not have to retype a reply the server rejected.
    expect(wrapper.get<HTMLTextAreaElement>('[data-testid="reply-input"]').element.value).toBe(
      'oops',
    )
    wrapper.unmount()
  })
})
