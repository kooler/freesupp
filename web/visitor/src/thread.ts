import type { Thread, ThreadMessage } from './api'

export type Bubble = {
  id: number
  side: 'visitor' | 'support'
  author: string
  body: string
  time: string
  /** false when the previous bubble came from the same side. */
  showAuthor: boolean
}

export type FormatOptions = {
  now?: Date
  timeZone?: string
  locale?: string
}

/** toBubbles turns a thread into the rows the page renders, oldest first. */
export function toBubbles(thread: Thread, opts: FormatOptions = {}): Bubble[] {
  let prevSide: string | null = null
  return thread.messages.map((m) => {
    const side = m.sender === 'visitor' ? 'visitor' : 'support'
    const bubble: Bubble = {
      id: m.id,
      side,
      author: authorLabel(m, thread),
      body: m.body,
      time: formatTimestamp(m.created_at, opts),
      showAuthor: side !== prevSide,
    }
    prevSide = side
    return bubble
  })
}

function authorLabel(m: ThreadMessage, thread: Thread): string {
  if (m.sender === 'operator') return m.author || 'Support'
  return m.author || thread.visitor_name || 'You'
}

/**
 * formatTimestamp keeps timestamps short: time only for today, day and month
 * within the current year, otherwise the full date.
 */
export function formatTimestamp(iso: string, opts: FormatOptions = {}): string {
  const at = new Date(iso)
  if (Number.isNaN(at.getTime())) return ''

  const now = opts.now ?? new Date()
  const locale = opts.locale ?? 'en-GB'
  const base: Intl.DateTimeFormatOptions = { hour: '2-digit', minute: '2-digit', hour12: false }
  if (opts.timeZone) base.timeZone = opts.timeZone

  const parts = (o: Intl.DateTimeFormatOptions) =>
    new Intl.DateTimeFormat(locale, { ...base, ...o }).format(at)

  // Both comparisons read the chosen zone's yyyy-mm-dd; getFullYear would use
  // the host zone and disagree with the date rendered next to it.
  const day = (d: Date) => new Intl.DateTimeFormat('en-CA', { timeZone: opts.timeZone }).format(d)
  const year = (d: Date) => day(d).slice(0, 4)
  if (day(at) === day(now)) return parts({})
  if (year(at) === year(now)) return parts({ day: 'numeric', month: 'short' })
  return parts({ day: 'numeric', month: 'short', year: 'numeric' })
}

/** an archived thread still accepts a follow-up — it opens a fresh conversation. */
export function isArchived(thread: Thread | null): boolean {
  return thread?.status === 'archived'
}
