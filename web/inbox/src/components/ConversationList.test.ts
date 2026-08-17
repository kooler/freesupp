import { describe, expect, it } from 'vitest'
import { mount } from '@vue/test-utils'
import ConversationList from './ConversationList.vue'
import type { ConversationSummary } from '@/api'

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

function render(conversations: ConversationSummary[], activeId: number | null = null) {
  return mount(ConversationList, { props: { conversations, activeId, loading: false } })
}

describe('ConversationList', () => {
  it('renders rows newest activity first', () => {
    const wrapper = render([
      conv({ id: 1, last_message_at: '2026-08-14T09:00:00Z' }),
      conv({ id: 2, last_message_at: '2026-08-14T11:00:00Z' }),
    ])
    const ids = wrapper.findAll('button').map((b) => b.attributes('data-testid'))
    expect(ids).toEqual(['row-2', 'row-1'])
  })

  it('marks unread rows and shows the dot', () => {
    const wrapper = render([conv({ id: 1, unread: true }), conv({ id: 2 })])

    expect(wrapper.get('[data-testid="row-1"]').attributes('data-unread')).toBe('true')
    expect(wrapper.get('[data-testid="row-2"]').attributes('data-unread')).toBe('false')
    expect(wrapper.findAll('[data-testid="unread-dot"]')).toHaveLength(1)
    expect(wrapper.get('[data-testid="row-1"]').html()).toContain('font-semibold')
  })

  it('prefixes operator snippets with "You:"', () => {
    const wrapper = render([conv({ last_sender: 'operator' })])
    expect(wrapper.get('[data-testid="row-1"]').text()).toContain('You:')
  })

  it('falls back to the email when the visitor has no name', () => {
    const wrapper = render([conv({ visitor_name: '' })])
    expect(wrapper.get('[data-testid="row-1"]').text()).toContain('visitor@example.com')
  })

  it('marks the active row', () => {
    const wrapper = render([conv({ id: 1 }), conv({ id: 2 })], 2)
    expect(wrapper.get('[data-testid="row-2"]').attributes('aria-current')).toBe('true')
    expect(wrapper.get('[data-testid="row-1"]').attributes('aria-current')).toBeUndefined()
  })

  it('emits the selected id', async () => {
    const wrapper = render([conv({ id: 7 })])
    await wrapper.get('[data-testid="row-7"]').trigger('click')
    expect(wrapper.emitted('select')).toEqual([[7]])
  })

  it('shows an empty state', () => {
    expect(render([]).text()).toContain('Nothing here yet.')
  })

  it('shows a loading state only while the list is still empty', () => {
    const loading = mount(ConversationList, {
      props: { conversations: [], activeId: null, loading: true },
    })
    expect(loading.text()).toContain('Loading…')

    const loaded = mount(ConversationList, {
      props: { conversations: [conv()], activeId: null, loading: true },
    })
    expect(loaded.text()).not.toContain('Loading…')
  })
})
