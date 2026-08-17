export type ConversationStatus = 'open' | 'archived'

export type ConversationSummary = {
  id: number
  visitor_email: string
  visitor_name: string
  status: ConversationStatus
  unread: boolean
  snippet: string
  last_sender: 'visitor' | 'operator'
  created_at: string
  last_message_at: string
}

export type InboxMessage = {
  id: number
  sender: 'visitor' | 'operator'
  operator_email?: string
  body: string
  created_at: string
}

export type ConversationDetail = {
  id: number
  visitor_email: string
  visitor_name: string
  token: string
  status: ConversationStatus
  unread: boolean
  created_at: string
  last_message_at: string
  messages: InboxMessage[]
}

/** ApiError carries the server's human-readable message plus its status. */
export class ApiError extends Error {
  readonly status: number

  constructor(status: number, message: string) {
    super(message)
    this.name = 'ApiError'
    this.status = status
  }

  get unauthorized(): boolean {
    return this.status === 401
  }

  get notFound(): boolean {
    return this.status === 404
  }
}

const genericError = 'Something went wrong. Please try again.'

let unauthorizedHandler: (() => void) | null = null

/**
 * onUnauthorized registers what to do when the session is gone — the app uses
 * it to drop back to the login screen from any request, including background
 * polls.
 */
export function onUnauthorized(handler: (() => void) | null): void {
  unauthorizedHandler = handler
}

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
    if (res.status === 401) unauthorizedHandler?.()
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

export type Me = {
  email: string
  is_admin: boolean
}

export type AuthConfig = {
  setup_required: boolean
}

export type InboxUser = {
  id: number
  email: string
  is_admin: boolean
  created_at: string
}

/** fetchMe returns the signed-in operator, or null when there is no session. */
export async function fetchMe(): Promise<Me | null> {
  try {
    return await request<Me>('/api/me')
  } catch (err) {
    if (err instanceof ApiError && err.unauthorized) return null
    throw err
  }
}

/** fetchAuthConfig reports whether the first-run setup screen is due. */
export function fetchAuthConfig(): Promise<AuthConfig> {
  return request<AuthConfig>('/api/auth/config')
}

export function loginWithPassword(email: string, password: string): Promise<Me> {
  return request<Me>('/api/auth/login', {
    method: 'POST',
    body: JSON.stringify({ email: email.trim(), password }),
  })
}

/** setupAccount creates the first admin on a fresh install and signs them in. */
export function setupAccount(email: string, password: string): Promise<Me> {
  return request<Me>('/api/auth/setup', {
    method: 'POST',
    body: JSON.stringify({ email: email.trim(), password }),
  })
}

export async function listUsers(): Promise<InboxUser[]> {
  const res = await request<{ users: InboxUser[] }>('/api/users')
  return res?.users ?? []
}

export function createUser(email: string, password: string, isAdmin: boolean): Promise<InboxUser> {
  return request<InboxUser>('/api/users', {
    method: 'POST',
    body: JSON.stringify({ email: email.trim(), password, is_admin: isAdmin }),
  })
}

export async function deleteUser(id: number): Promise<void> {
  await request<null>(`/api/users/${id}`, { method: 'DELETE' })
}

export function setUserAdmin(id: number, isAdmin: boolean): Promise<InboxUser> {
  return request<InboxUser>(`/api/users/${id}/admin`, {
    method: 'PUT',
    body: JSON.stringify({ is_admin: isAdmin }),
  })
}

export async function resetUserPassword(id: number, password: string): Promise<void> {
  await request<null>(`/api/users/${id}/password`, {
    method: 'PUT',
    body: JSON.stringify({ password }),
  })
}

export async function changeMyPassword(currentPassword: string, newPassword: string): Promise<void> {
  await request<null>('/api/me/password', {
    method: 'PUT',
    body: JSON.stringify({ current_password: currentPassword, new_password: newPassword }),
  })
}

export async function listConversations(
  status: ConversationStatus,
): Promise<ConversationSummary[]> {
  const res = await request<{ conversations: ConversationSummary[] }>(
    `/api/inbox/conversations?status=${encodeURIComponent(status)}`,
  )
  return res?.conversations ?? []
}

export function getConversation(id: number): Promise<ConversationDetail> {
  return request<ConversationDetail>(`/api/inbox/conversations/${id}`)
}

export function replyToConversation(id: number, message: string): Promise<InboxMessage> {
  return request<InboxMessage>(`/api/inbox/conversations/${id}/reply`, {
    method: 'POST',
    body: JSON.stringify({ message: message.trim() }),
  })
}

export function setArchived(id: number, archived: boolean): Promise<ConversationDetail> {
  const action = archived ? 'archive' : 'unarchive'
  return request<ConversationDetail>(`/api/inbox/conversations/${id}/${action}`, { method: 'POST' })
}

export async function logout(): Promise<void> {
  await request<null>('/auth/logout', { method: 'POST' })
}
