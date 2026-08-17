export type ThreadMessage = {
  id: number
  sender: 'visitor' | 'operator'
  author: string
  body: string
  created_at: string
}

export type Thread = {
  token: string
  status: 'open' | 'archived'
  visitor_email: string
  visitor_name: string
  created_at: string
  messages: ThreadMessage[]
}

export type PublicConfig = {
  turnstileSiteKey: string
}

export type Submission = {
  email: string
  name: string
  message: string
  turnstileToken?: string
}

/** ApiError carries the server's human-readable message plus its status. */
export class ApiError extends Error {
  readonly status: number

  constructor(status: number, message: string) {
    super(message)
    this.name = 'ApiError'
    this.status = status
  }

  get notFound(): boolean {
    return this.status === 404
  }

  get rateLimited(): boolean {
    return this.status === 429
  }
}

const genericError = 'Something went wrong. Please try again.'

async function request<T>(path: string, init?: RequestInit): Promise<T> {
  let res: Response
  try {
    res = await fetch(path, {
      ...init,
      headers: { 'Content-Type': 'application/json', ...(init?.headers ?? {}) },
    })
  } catch {
    throw new ApiError(0, 'Could not reach the server. Please check your connection.')
  }

  const body = await readJSON(res)
  if (!res.ok) {
    const msg = typeof body?.error === 'string' && body.error ? body.error : genericError
    throw new ApiError(res.status, msg)
  }
  return body as T
}

async function readJSON(res: Response): Promise<any> {
  const text = await res.text()
  if (!text) return null
  try {
    return JSON.parse(text)
  } catch {
    return null
  }
}

/**
 * fetchConfig reads the public settings. A missing endpoint is not fatal — it
 * only means no captcha is configured.
 */
export async function fetchConfig(): Promise<PublicConfig> {
  try {
    const cfg = await request<{ turnstile_site_key?: string }>('/api/config')
    return { turnstileSiteKey: cfg?.turnstile_site_key ?? '' }
  } catch {
    return { turnstileSiteKey: '' }
  }
}

/**
 * submitMessage sends the contact form. It resolves without a thread token:
 * nothing here proves the sender owns the address, so the server emails the
 * magic link to that address instead of returning it.
 */
export async function submitMessage(input: Submission): Promise<void> {
  await request<unknown>('/api/messages', {
    method: 'POST',
    body: JSON.stringify({
      email: input.email.trim(),
      name: input.name.trim(),
      message: input.message.trim(),
      turnstile_token: input.turnstileToken ?? '',
    }),
  })
}

export function fetchThread(token: string): Promise<Thread> {
  return request<Thread>(`/api/thread/${encodeURIComponent(token)}`)
}

/**
 * replyToThread appends a follow-up. The returned token differs from the one
 * passed in when the old conversation was archived and the store started a new
 * one, so callers must follow it.
 */
export async function replyToThread(token: string, message: string): Promise<string> {
  const res = await request<{ token: string }>(
    `/api/thread/${encodeURIComponent(token)}/messages`,
    { method: 'POST', body: JSON.stringify({ message: message.trim() }) },
  )
  return res.token
}
