import { flushPromises, mount } from '@vue/test-utils'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import { ApiError } from '../api'
import WidgetForm from './WidgetForm.vue'

const fetchConfig = vi.fn()
const submitMessage = vi.fn()
const renderCaptcha = vi.fn()
const resetCaptcha = vi.fn()

vi.mock('../api', async (importOriginal) => ({
  ...(await importOriginal<typeof import('../api')>()),
  fetchConfig: (...args: unknown[]) => fetchConfig(...args),
  submitMessage: (...args: unknown[]) => submitMessage(...args),
}))

vi.mock('../turnstile', () => ({
  loadTurnstile: () => Promise.resolve({ render: renderCaptcha, reset: resetCaptcha }),
}))

async function mountForm() {
  const wrapper = mount(WidgetForm)
  await flushPromises()
  return wrapper
}

async function fill(wrapper: Awaited<ReturnType<typeof mountForm>>, values: Record<string, string>) {
  for (const [id, value] of Object.entries(values)) {
    await wrapper.find(`#${id}`).setValue(value)
  }
}

beforeEach(() => {
  fetchConfig.mockReset().mockResolvedValue({ turnstileSiteKey: '' })
  submitMessage.mockReset().mockResolvedValue(undefined)
  renderCaptcha.mockReset().mockReturnValue('widget-1')
  resetCaptcha.mockReset()
})

describe('WidgetForm', () => {
  it('blocks submission and shows errors for invalid input', async () => {
    const wrapper = await mountForm()
    await fill(wrapper, { 'fs-email': 'nope', 'fs-message': '  ' })
    await wrapper.find('form').trigger('submit')
    await flushPromises()

    expect(submitMessage).not.toHaveBeenCalled()
    expect(wrapper.text()).toContain('Please enter a valid email address.')
    expect(wrapper.text()).toContain('Please write a message.')
  })

  it('submits and confirms the reply address on success', async () => {
    const wrapper = await mountForm()
    await fill(wrapper, {
      'fs-email': 'visitor@example.com',
      'fs-name': 'Ada',
      'fs-message': 'my printer is on fire',
    })
    await wrapper.find('form').trigger('submit')
    await flushPromises()

    expect(submitMessage).toHaveBeenCalledWith({
      email: 'visitor@example.com',
      name: 'Ada',
      message: 'my printer is on fire',
      turnstileToken: '',
    })
    expect(wrapper.text()).toContain('Thanks')
    expect(wrapper.text()).toContain('visitor@example.com')
    // The magic link is emailed, never shown here: nothing proves the person
    // at this form owns the address they typed.
    expect(wrapper.find('a.fs-link').exists()).toBe(false)
    expect(wrapper.find('form').exists()).toBe(false)
  })

  it('shows the server message when the submission fails', async () => {
    submitMessage.mockRejectedValue(new ApiError(503, 'could not verify the captcha'))
    const wrapper = await mountForm()
    await fill(wrapper, { 'fs-email': 'visitor@example.com', 'fs-message': 'hello' })
    await wrapper.find('form').trigger('submit')
    await flushPromises()

    expect(wrapper.find('.fs-alert').text()).toBe('could not verify the captcha')
    expect(wrapper.find('form').exists()).toBe(true)
  })

  it('skips the captcha entirely when no site key is configured', async () => {
    await mountForm()
    expect(renderCaptcha).not.toHaveBeenCalled()
  })

  it('renders the captcha and requires a token when a site key is configured', async () => {
    fetchConfig.mockResolvedValue({ turnstileSiteKey: 'site-key' })
    const wrapper = await mountForm()
    expect(renderCaptcha).toHaveBeenCalledOnce()
    expect(renderCaptcha.mock.calls[0][1].sitekey).toBe('site-key')

    await fill(wrapper, { 'fs-email': 'visitor@example.com', 'fs-message': 'hello' })
    await wrapper.find('form').trigger('submit')
    await flushPromises()
    expect(submitMessage).not.toHaveBeenCalled()
    expect(wrapper.text()).toContain('Please complete the captcha.')

    // The widget callback delivers a token, after which the submission goes through.
    renderCaptcha.mock.calls[0][1].callback('cf-token')
    await wrapper.find('form').trigger('submit')
    await flushPromises()
    expect(submitMessage).toHaveBeenCalledWith(expect.objectContaining({ turnstileToken: 'cf-token' }))
  })

  // Turnstile tokens are single-use. Without a reset the widget still reads
  // "Success!" but never fires its callback again, so the visitor is stuck on
  // "Please complete the captcha." with no way forward.
  it('resets the captcha widget so a failed submission can be retried', async () => {
    fetchConfig.mockResolvedValue({ turnstileSiteKey: 'site-key' })
    submitMessage.mockRejectedValueOnce(new ApiError(429, 'too many requests'))
    const wrapper = await mountForm()
    renderCaptcha.mock.calls[0][1].callback('cf-token')

    await fill(wrapper, { 'fs-email': 'visitor@example.com', 'fs-message': 'hello' })
    await wrapper.find('form').trigger('submit')
    await flushPromises()

    expect(wrapper.find('.fs-alert').text()).toBe('too many requests')
    expect(resetCaptcha).toHaveBeenCalledWith('widget-1')

    // A fresh challenge yields a new token and the retry goes through.
    submitMessage.mockResolvedValue('tok-2')
    renderCaptcha.mock.calls[0][1].callback('cf-token-2')
    await wrapper.find('form').trigger('submit')
    await flushPromises()

    expect(submitMessage).toHaveBeenLastCalledWith(
      expect.objectContaining({ turnstileToken: 'cf-token-2' }),
    )
    expect(wrapper.text()).toContain('Thanks')
  })
})
