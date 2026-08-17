import { describe, expect, it, vi } from 'vitest'
import { flushPromises, mount } from '@vue/test-utils'
import ConversationDetail from './ConversationDetail.vue'
import type { ConversationDetail as Detail } from '@/api'

function detail(over: Partial<Detail> = {}): Detail {
  return {
    id: 1,
    visitor_email: 'visitor@example.com',
    visitor_name: 'Ada Lovelace',
    token: 'tok',
    status: 'open',
    unread: false,
    created_at: '2026-08-14T10:00:00Z',
    last_message_at: '2026-08-14T10:05:00Z',
    messages: [
      { id: 1, sender: 'visitor', body: 'hello', created_at: '2026-08-14T10:00:00Z' },
      {
        id: 2,
        sender: 'operator',
        operator_email: 'ops@example.com',
        body: 'on it',
        created_at: '2026-08-14T10:05:00Z',
      },
    ],
    ...over,
  }
}

/** Resolves true (stored) unless a test overrides `reply`. */
function render(conversation: Detail | null, over: Record<string, unknown> = {}) {
  const reply = vi.fn(async () => true)
  const wrapper = mount(ConversationDetail, {
    props: { conversation, loading: false, error: '', sending: false, reply, ...over },
  })
  return { wrapper, reply: (wrapper.props('reply') as typeof reply) }
}

function draftValue(wrapper: ReturnType<typeof render>['wrapper']) {
  return wrapper.get<HTMLTextAreaElement>('[data-testid="reply-input"]').element.value
}

describe('ConversationDetail', () => {
  it('prompts when nothing is selected', () => {
    expect(render(null).wrapper.text()).toContain('Select a conversation')
  })

  // With no conversation there is no footer, so the error has nowhere else to go.
  it('shows an error even when no conversation is loaded', () => {
    const { wrapper } = render(null, { error: 'This conversation no longer exists.' })
    expect(wrapper.get('[data-testid="detail-error"]').text()).toBe(
      'This conversation no longer exists.',
    )
    expect(wrapper.text()).not.toContain('Select a conversation')
  })

  it('renders the history with both sides', () => {
    const { wrapper } = render(detail())
    const sides = wrapper.findAll('[data-side]').map((el) => el.attributes('data-side'))
    expect(sides).toEqual(['visitor', 'operator'])
    expect(wrapper.text()).toContain('hello')
    expect(wrapper.text()).toContain('ops@example.com')
  })

  it('sends on click and clears the draft', async () => {
    const { wrapper, reply } = render(detail())
    await wrapper.get('[data-testid="reply-input"]').setValue('thanks!')
    await wrapper.get('[data-testid="send-reply"]').trigger('click')
    await flushPromises()

    expect(reply.mock.calls).toEqual([['thanks!']])
    expect(draftValue(wrapper)).toBe('')
  })

  it('keeps the draft when the reply could not be sent', async () => {
    const { wrapper } = render(detail(), { reply: vi.fn(async () => false) })
    await wrapper.get('[data-testid="reply-input"]').setValue('please do not eat this')
    await wrapper.get('[data-testid="send-reply"]').trigger('click')
    await flushPromises()

    expect(draftValue(wrapper)).toBe('please do not eat this')
  })

  it.each([
    ['meta', { metaKey: true }],
    ['ctrl', { ctrlKey: true }],
  ])('sends on %s+Enter', async (_name, mods) => {
    const { wrapper, reply } = render(detail())
    await wrapper.get('[data-testid="reply-input"]').setValue('quick reply')
    await wrapper.get('[data-testid="reply-input"]').trigger('keydown', { key: 'Enter', ...mods })
    await flushPromises()

    expect(reply.mock.calls).toEqual([['quick reply']])
  })

  it('leaves plain Enter as a newline', async () => {
    const { wrapper, reply } = render(detail())
    await wrapper.get('[data-testid="reply-input"]').setValue('half a thought')
    await wrapper.get('[data-testid="reply-input"]').trigger('keydown', { key: 'Enter' })
    await flushPromises()

    expect(reply).not.toHaveBeenCalled()
  })

  it('refuses to send a blank reply', async () => {
    const { wrapper, reply } = render(detail())
    await wrapper.get('[data-testid="reply-input"]').setValue('   ')
    await wrapper.get('[data-testid="reply-input"]').trigger('keydown', { key: 'Enter', metaKey: true })
    await flushPromises()

    expect(reply).not.toHaveBeenCalled()
    expect(wrapper.get('[data-testid="send-reply"]').attributes('disabled')).toBeDefined()
  })

  it('drops the draft when another conversation is opened', async () => {
    const { wrapper } = render(detail())
    await wrapper.get('[data-testid="reply-input"]').setValue('for conversation 1')
    await wrapper.setProps({ conversation: detail({ id: 2 }) })

    expect(draftValue(wrapper)).toBe('')
  })

  it('toggles between archive and unarchive', async () => {
    const open = render(detail()).wrapper
    expect(open.text()).toContain('Archive')
    await open.findAll('button').find((b) => b.text() === 'Archive')!.trigger('click')
    expect(open.emitted('archive')).toEqual([[true]])

    const archived = render(detail({ status: 'archived' })).wrapper
    expect(archived.text()).toContain('Unarchive')
    await archived.findAll('button').find((b) => b.text() === 'Unarchive')!.trigger('click')
    expect(archived.emitted('archive')).toEqual([[false]])
  })

  it('shows an error and a sending state', () => {
    const { wrapper } = render(detail(), { error: 'could not send your reply', sending: true })
    expect(wrapper.get('[data-testid="detail-error"]').text()).toBe('could not send your reply')
    expect(wrapper.get('[data-testid="send-reply"]').text()).toContain('Sending')
  })
})
