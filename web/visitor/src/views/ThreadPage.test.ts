import { flushPromises, mount } from '@vue/test-utils'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import { ApiError, type Thread } from '../api'
import ThreadPage from './ThreadPage.vue'

const fetchThread = vi.fn()
const replyToThread = vi.fn()

vi.mock('../api', async (importOriginal) => ({
  ...(await importOriginal<typeof import('../api')>()),
  fetchThread: (...args: unknown[]) => fetchThread(...args),
  replyToThread: (...args: unknown[]) => replyToThread(...args),
}))

const thread: Thread = {
  token: 'tok-1',
  status: 'open',
  visitor_email: 'visitor@example.com',
  visitor_name: 'Ada',
  created_at: '2026-08-14T09:00:00Z',
  messages: [
    {
      id: 1,
      sender: 'visitor',
      author: 'Ada',
      body: 'my printer is on fire',
      created_at: '2026-08-14T09:00:00Z',
    },
    {
      id: 2,
      sender: 'operator',
      author: 'Support',
      body: 'have you tried water',
      created_at: '2026-08-14T09:30:00Z',
    },
  ],
}

async function mountPage(token = 'tok-1') {
  const wrapper = mount(ThreadPage, { props: { token } })
  await flushPromises()
  return wrapper
}

beforeEach(() => {
  fetchThread.mockReset().mockResolvedValue(thread)
  replyToThread.mockReset().mockResolvedValue('tok-1')
  // One test replaceStates to a new token; reset so no test inherits that path.
  history.replaceState(null, '', '/')
})

describe('ThreadPage', () => {
  it('renders the history with both sides', async () => {
    const wrapper = await mountPage()
    expect(fetchThread).toHaveBeenCalledWith('tok-1')

    const bubbles = wrapper.findAll('.fs-bubble')
    expect(bubbles).toHaveLength(2)
    expect(bubbles[0].classes()).toContain('fs-bubble--visitor')
    expect(bubbles[0].text()).toContain('my printer is on fire')
    expect(bubbles[1].classes()).toContain('fs-bubble--support')
    expect(bubbles[1].text()).toContain('Support')
    expect(wrapper.text()).toContain('visitor@example.com')
  })

  it('shows an invalid-link state and hides the reply box for an unknown token', async () => {
    fetchThread.mockRejectedValue(new ApiError(404, 'this conversation link is not valid'))
    const wrapper = await mountPage('bogus')

    expect(wrapper.text()).toContain('This conversation link is not valid')
    expect(wrapper.find('form').exists()).toBe(false)
  })

  it('keeps the reply box on a transient load failure', async () => {
    fetchThread.mockRejectedValue(new ApiError(500, 'could not load this conversation'))
    const wrapper = await mountPage()

    expect(wrapper.text()).toContain('could not load this conversation')
    expect(wrapper.find('form').exists()).toBe(true)
  })

  it('rejects an empty follow-up without calling the API', async () => {
    const wrapper = await mountPage()
    await wrapper.find('textarea').setValue('   ')
    await wrapper.find('form').trigger('submit')
    await flushPromises()

    expect(replyToThread).not.toHaveBeenCalled()
    expect(wrapper.text()).toContain('Please write a message.')
  })

  it('sends a follow-up and reloads the thread', async () => {
    const wrapper = await mountPage()
    await wrapper.find('textarea').setValue('still burning')
    await wrapper.find('form').trigger('submit')
    await flushPromises()

    expect(replyToThread).toHaveBeenCalledWith('tok-1', 'still burning')
    expect(fetchThread).toHaveBeenCalledTimes(2)
    expect(fetchThread).toHaveBeenLastCalledWith('tok-1')
    expect((wrapper.find('textarea').element as HTMLTextAreaElement).value).toBe('')
  })

  it('follows the new token when an archived thread spawns a new conversation', async () => {
    fetchThread.mockResolvedValue({ ...thread, status: 'archived' })
    replyToThread.mockResolvedValue('tok-2')
    const wrapper = await mountPage()
    expect(wrapper.text()).toContain('This conversation was closed')

    await wrapper.find('textarea').setValue('one more thing')
    await wrapper.find('form').trigger('submit')
    await flushPromises()

    expect(fetchThread).toHaveBeenLastCalledWith('tok-2')
    expect(location.pathname).toBe('/t/tok-2')
  })

  it('shows the server error when sending fails', async () => {
    replyToThread.mockRejectedValue(new ApiError(429, 'too many requests'))
    const wrapper = await mountPage()
    await wrapper.find('textarea').setValue('hello?')
    await wrapper.find('form').trigger('submit')
    await flushPromises()

    expect(wrapper.text()).toContain('too many requests')
  })
})
