import { describe, expect, it } from 'vitest'
import type { ConversationSummary, InboxMessage } from './api'
import {
  displayName,
  initials,
  messageTime,
  pageTitle,
  relativeTime,
  sortByActivity,
  toBubbles,
  toRows,
  unreadCount,
} from './presentation'

const now = new Date('2026-08-14T12:00:00Z')
// Pinned so "today" and "same year" mean the same thing wherever the suite runs.
const utc = { timeZone: 'UTC' }

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

describe('displayName', () => {
  it('prefers the name', () => {
    expect(displayName(conv())).toBe('Ada Lovelace')
  })

  it('falls back to the email when the name is blank', () => {
    expect(displayName(conv({ visitor_name: '   ' }))).toBe('visitor@example.com')
  })
})

describe('relativeTime', () => {
  it.each([
    ['2026-08-14T11:59:30Z', 'just now'],
    ['2026-08-14T11:55:00Z', '5m'],
    ['2026-08-14T09:00:00Z', '3h'],
    ['2026-08-13T09:00:00Z', 'yesterday'],
    ['2026-08-11T09:00:00Z', '3d'],
    ['2026-08-01T09:00:00Z', '1 Aug'],
    ['2025-12-24T09:00:00Z', '24 Dec 2025'],
  ])('%s reads as %s', (iso, expected) => {
    expect(relativeTime(iso, now, utc)).toBe(expected)
  })

  it('returns nothing for an unparsable stamp', () => {
    expect(relativeTime('not-a-date', now, utc)).toBe('')
  })

  it('decides "same year" in the requested zone, not the host zone', () => {
    // 1 Jan 2026 UTC is still 2025 in Honolulu.
    expect(relativeTime('2026-01-01T05:00:00Z', now, { timeZone: 'UTC' })).toBe('1 Jan')
    expect(relativeTime('2026-01-01T05:00:00Z', now, { timeZone: 'Pacific/Honolulu' })).toBe(
      '31 Dec 2025',
    )
  })
})

describe('messageTime', () => {
  it('shows time only for today', () => {
    expect(messageTime('2026-08-14T09:05:00Z', now, utc)).toMatch(/^\d{2}:\d{2}$/)
  })

  it('adds the day for older messages in the same year', () => {
    expect(messageTime('2026-08-01T09:05:00Z', now, utc)).toContain('1 Aug')
  })

  it('adds the year for older messages', () => {
    expect(messageTime('2025-08-01T09:05:00Z', now, utc)).toContain('2025')
  })

  it('decides "today" in the requested zone, not the host zone', () => {
    // 09:05 UTC on the 14th is still the 13th in Honolulu, so it is not "today".
    expect(messageTime('2026-08-14T09:05:00Z', now, { timeZone: 'Pacific/Honolulu' })).toContain(
      'Aug',
    )
  })

  it('returns nothing for an unparsable stamp', () => {
    expect(messageTime('nope', now, utc)).toBe('')
  })
})

describe('ordering and unread', () => {
  it('sorts newest activity first without mutating the input', () => {
    const input = [
      conv({ id: 1, last_message_at: '2026-08-14T09:00:00Z' }),
      conv({ id: 2, last_message_at: '2026-08-14T11:00:00Z' }),
      conv({ id: 3, last_message_at: '2026-08-14T10:00:00Z' }),
    ]
    expect(sortByActivity(input).map((c) => c.id)).toEqual([2, 3, 1])
    expect(input.map((c) => c.id)).toEqual([1, 2, 3])
  })

  // The server orders by `last_message_at DESC, id DESC`; re-sorting the page
  // client-side must not reshuffle rows that share a timestamp.
  it('breaks ties on activity by highest id, matching the server', () => {
    const same = '2026-08-14T11:00:00Z'
    const input = [
      conv({ id: 2, last_message_at: same }),
      conv({ id: 5, last_message_at: same }),
      conv({ id: 3, last_message_at: same }),
    ]
    expect(sortByActivity(input).map((c) => c.id)).toEqual([5, 3, 2])
  })

  it('counts unread conversations', () => {
    expect(unreadCount([conv({ unread: true }), conv(), conv({ unread: true })])).toBe(2)
  })

  it('puts the unread count in the page title', () => {
    expect(pageTitle(0)).toBe('Inbox')
    expect(pageTitle(3)).toBe('(3) Inbox')
  })
})

describe('toRows', () => {
  it('builds ordered rows with unread flags and relative times', () => {
    const rows = toRows(
      [
        conv({ id: 1, last_message_at: '2026-08-14T09:00:00Z' }),
        conv({ id: 2, unread: true, last_message_at: '2026-08-14T11:55:00Z' }),
      ],
      now,
    )

    expect(rows.map((r) => r.id)).toEqual([2, 1])
    expect(rows[0]).toMatchObject({ name: 'Ada Lovelace', time: '5m', unread: true })
    expect(rows[1]!.unread).toBe(false)
  })

  it('marks rows whose last message came from an operator', () => {
    const rows = toRows([conv({ last_sender: 'operator' })], now)
    expect(rows[0]!.fromOperator).toBe(true)
  })
})

describe('toBubbles', () => {
  const visitor = { visitor_name: 'Ada Lovelace', visitor_email: 'visitor@example.com' }

  const messages: InboxMessage[] = [
    { id: 1, sender: 'visitor', body: 'hello', created_at: '2026-08-14T10:00:00Z' },
    { id: 2, sender: 'visitor', body: 'anyone?', created_at: '2026-08-14T10:01:00Z' },
    {
      id: 3,
      sender: 'operator',
      operator_email: 'ops@example.com',
      body: 'on it',
      created_at: '2026-08-14T10:05:00Z',
    },
  ]

  it('labels each side and repeats the author only when it changes', () => {
    const bubbles = toBubbles(messages, visitor, now)
    expect(bubbles.map((b) => b.showAuthor)).toEqual([true, false, true])
    expect(bubbles[0]!.author).toBe('Ada Lovelace')
    expect(bubbles[2]).toMatchObject({ side: 'operator', author: 'ops@example.com' })
  })

  it('falls back to "Support" when the operator email is missing', () => {
    const bubbles = toBubbles(
      [{ id: 9, sender: 'operator', body: 'hi', created_at: '2026-08-14T10:00:00Z' }],
      visitor,
      now,
    )
    expect(bubbles[0]!.author).toBe('Support')
  })

  it('handles an empty history', () => {
    expect(toBubbles([], visitor, now)).toEqual([])
  })
})

describe('initials', () => {
  it.each([
    ['Ada Lovelace', 'AL'],
    ['ada', 'A'],
    ['visitor@example.com', 'VE'],
    ['   ', '?'],
  ])('%s → %s', (name, expected) => {
    expect(initials(name)).toBe(expected)
  })
})
