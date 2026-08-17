import type { ConversationSummary, InboxMessage } from './api'

/** displayName prefers the visitor's name and falls back to their email. */
export function displayName(conv: {
  visitor_name: string
  visitor_email: string
}): string {
  return conv.visitor_name.trim() || conv.visitor_email
}

/**
 * timeZone is the IANA zone all formatting resolves in. It defaults to the
 * operator's own zone; tests pin it so "today" and "same year" do not depend on
 * where the suite runs.
 */
export type FormatOptions = { timeZone?: string }

/** calendarDay is the yyyy-mm-dd a stamp falls on in the chosen zone. */
function calendarDay(at: Date, timeZone?: string): string {
  return new Intl.DateTimeFormat('en-CA', timeZone ? { timeZone } : {}).format(at)
}

/** year in the chosen zone, taken from the same yyyy-mm-dd rendering. */
function calendarYear(at: Date, timeZone?: string): string {
  return calendarDay(at, timeZone).slice(0, 4)
}

/**
 * relativeTime keeps the list scannable: minutes and hours for recent
 * activity, then a short date. The server already orders rows, this is only
 * how the timestamp reads.
 */
export function relativeTime(iso: string, now: Date = new Date(), opts: FormatOptions = {}): string {
  const at = new Date(iso)
  if (Number.isNaN(at.getTime())) return ''

  const seconds = Math.round((now.getTime() - at.getTime()) / 1000)
  if (seconds < 60) return 'just now'
  const minutes = Math.floor(seconds / 60)
  if (minutes < 60) return `${minutes}m`
  const hours = Math.floor(minutes / 60)
  if (hours < 24) return `${hours}h`
  const days = Math.floor(hours / 24)
  if (days === 1) return 'yesterday'
  if (days < 7) return `${days}d`

  const sameYear = calendarYear(at, opts.timeZone) === calendarYear(now, opts.timeZone)
  return new Intl.DateTimeFormat('en-GB', {
    day: 'numeric',
    month: 'short',
    ...(sameYear ? {} : { year: 'numeric' }),
    ...(opts.timeZone ? { timeZone: opts.timeZone } : {}),
  }).format(at)
}

/** messageTime is the absolute stamp shown on a bubble in the detail pane. */
export function messageTime(iso: string, now: Date = new Date(), opts: FormatOptions = {}): string {
  const at = new Date(iso)
  if (Number.isNaN(at.getTime())) return ''

  const time: Intl.DateTimeFormatOptions = {
    hour: '2-digit',
    minute: '2-digit',
    hour12: false,
    ...(opts.timeZone ? { timeZone: opts.timeZone } : {}),
  }
  if (calendarDay(at, opts.timeZone) === calendarDay(now, opts.timeZone)) {
    return new Intl.DateTimeFormat('en-GB', time).format(at)
  }
  return new Intl.DateTimeFormat('en-GB', {
    day: 'numeric',
    month: 'short',
    ...(calendarYear(at, opts.timeZone) === calendarYear(now, opts.timeZone)
      ? {}
      : { year: 'numeric' }),
    ...time,
  }).format(at)
}

/**
 * sortByActivity is newest activity first — the order the inbox list uses. Ties
 * fall back to the highest id, matching the server's `last_message_at DESC,
 * id DESC` so a re-sorted page keeps the order it was served in.
 */
export function sortByActivity<T extends { last_message_at: string; id: number }>(convs: T[]): T[] {
  return [...convs].sort((a, b) => {
    const byActivity = new Date(b.last_message_at).getTime() - new Date(a.last_message_at).getTime()
    return byActivity !== 0 ? byActivity : b.id - a.id
  })
}

export function unreadCount(convs: Pick<ConversationSummary, 'unread'>[]): number {
  return convs.reduce((n, c) => (c.unread ? n + 1 : n), 0)
}

/** pageTitle surfaces the unread count in the browser tab. */
export function pageTitle(unread: number): string {
  return unread > 0 ? `(${unread}) Inbox` : 'Inbox'
}

export type Row = {
  id: number
  name: string
  email: string
  snippet: string
  time: string
  unread: boolean
  /** true when the last message came from an operator, shown as "You: …". */
  fromOperator: boolean
}

/** toRows is everything the list needs to render a conversation. */
export function toRows(
  convs: ConversationSummary[],
  now: Date = new Date(),
  opts: FormatOptions = {},
): Row[] {
  return sortByActivity(convs).map((c) => ({
    id: c.id,
    name: displayName(c),
    email: c.visitor_email,
    snippet: c.snippet,
    time: relativeTime(c.last_message_at, now, opts),
    unread: c.unread,
    fromOperator: c.last_sender === 'operator',
  }))
}

export type Bubble = {
  id: number
  side: 'visitor' | 'operator'
  author: string
  body: string
  time: string
  /** false when the previous bubble came from the same side. */
  showAuthor: boolean
}

/** toBubbles turns a history into detail-pane rows, oldest first. */
export function toBubbles(
  messages: InboxMessage[],
  visitor: { visitor_name: string; visitor_email: string },
  now: Date = new Date(),
  opts: FormatOptions = {},
): Bubble[] {
  let prevSide: string | null = null
  return messages.map((m) => {
    const bubble: Bubble = {
      id: m.id,
      side: m.sender,
      author: m.sender === 'operator' ? m.operator_email || 'Support' : displayName(visitor),
      body: m.body,
      time: messageTime(m.created_at, now, opts),
      showAuthor: m.sender !== prevSide,
    }
    prevSide = m.sender
    return bubble
  })
}

/** initials feed the avatar circle in the list. */
export function initials(name: string): string {
  const parts = name.replace(/[^\p{L}\p{N}@. ]/gu, ' ').split(/[\s.@]+/u).filter(Boolean)
  if (parts.length === 0) return '?'
  const letters = parts.slice(0, 2).map((p) => p[0]!.toUpperCase())
  return letters.join('')
}
