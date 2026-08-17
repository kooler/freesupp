import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import {
  ApiError,
  fetchConfig,
  fetchThread,
  replyToThread,
  submitMessage,
  type Thread,
} from './api'

const fetchMock = vi.fn()

function reply(status: number, body: unknown) {
  const text = body === undefined ? '' : JSON.stringify(body)
  return Promise.resolve(new Response(text, { status }))
}

beforeEach(() => {
  fetchMock.mockReset()
  vi.stubGlobal('fetch', fetchMock)
})

afterEach(() => {
  vi.unstubAllGlobals()
})

describe('submitMessage', () => {
  it('posts trimmed fields', async () => {
    fetchMock.mockReturnValue(reply(201, { token: '' }))

    await submitMessage({
      email: '  Visitor@example.com ',
      name: ' Ada ',
      message: ' hello \n',
      turnstileToken: 'cf-token',
    })

    const [path, init] = fetchMock.mock.calls[0]
    expect(path).toBe('/api/messages')
    expect(init.method).toBe('POST')
    expect(JSON.parse(init.body)).toEqual({
      email: 'Visitor@example.com',
      name: 'Ada',
      message: 'hello',
      turnstile_token: 'cf-token',
    })
  })

  it('sends an empty captcha token when none was collected', async () => {
    fetchMock.mockReturnValue(reply(201, { token: '' }))
    await submitMessage({ email: 'v@example.com', name: '', message: 'hi' })
    expect(JSON.parse(fetchMock.mock.calls[0][1].body).turnstile_token).toBe('')
  })

  it('surfaces the server error message and status', async () => {
    fetchMock.mockReturnValue(reply(400, { error: 'please enter a valid email address' }))

    const err = await submitMessage({ email: 'x', name: '', message: 'hi' }).catch((e) => e)
    expect(err).toBeInstanceOf(ApiError)
    expect(err.status).toBe(400)
    expect(err.message).toBe('please enter a valid email address')
  })

  it('flags rate limiting', async () => {
    fetchMock.mockReturnValue(reply(429, { error: 'too many requests' }))
    const err = await submitMessage({ email: 'v@example.com', name: '', message: 'hi' }).catch(
      (e) => e,
    )
    expect(err.rateLimited).toBe(true)
  })

  it('falls back to a generic message when the body has no error field', async () => {
    fetchMock.mockReturnValue(reply(500, undefined))
    const err = await submitMessage({ email: 'v@example.com', name: '', message: 'hi' }).catch(
      (e) => e,
    )
    expect(err.message).toMatch(/something went wrong/i)
  })

  it('reports a network failure without a status', async () => {
    fetchMock.mockRejectedValue(new TypeError('failed to fetch'))
    const err = await submitMessage({ email: 'v@example.com', name: '', message: 'hi' }).catch(
      (e) => e,
    )
    expect(err.status).toBe(0)
    expect(err.message).toMatch(/could not reach the server/i)
  })
})

describe('fetchThread', () => {
  const thread: Thread = {
    token: 'tok',
    status: 'open',
    visitor_email: 'v@example.com',
    visitor_name: 'Ada',
    created_at: '2026-08-14T10:00:00Z',
    messages: [],
  }

  it('escapes the token in the path', async () => {
    fetchMock.mockReturnValue(reply(200, thread))
    await fetchThread('a b')
    expect(fetchMock.mock.calls[0][0]).toBe('/api/thread/a%20b')
  })

  it('marks an unknown token as not found', async () => {
    fetchMock.mockReturnValue(reply(404, { error: 'this conversation link is not valid' }))
    const err = await fetchThread('nope').catch((e) => e)
    expect(err.notFound).toBe(true)
  })
})

describe('replyToThread', () => {
  it('returns the token the server threaded the reply into', async () => {
    fetchMock.mockReturnValue(reply(201, { token: 'tok-2' }))
    expect(await replyToThread('tok-1', ' follow up ')).toBe('tok-2')
    expect(JSON.parse(fetchMock.mock.calls[0][1].body)).toEqual({ message: 'follow up' })
  })

  it('propagates validation errors', async () => {
    fetchMock.mockReturnValue(reply(400, { error: 'message cannot be empty' }))
    const err = await replyToThread('tok', ' ').catch((e) => e)
    expect(err.status).toBe(400)
  })
})

describe('fetchConfig', () => {
  it('reads the Turnstile site key', async () => {
    fetchMock.mockReturnValue(reply(200, { turnstile_site_key: 'site-key' }))
    expect(await fetchConfig()).toEqual({ turnstileSiteKey: 'site-key' })
  })

  it('treats a missing endpoint as no captcha', async () => {
    fetchMock.mockReturnValue(reply(404, undefined))
    expect(await fetchConfig()).toEqual({ turnstileSiteKey: '' })
  })

  it('treats a missing key as no captcha', async () => {
    fetchMock.mockReturnValue(reply(200, {}))
    expect(await fetchConfig()).toEqual({ turnstileSiteKey: '' })
  })
})
