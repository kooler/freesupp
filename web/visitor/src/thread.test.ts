import { describe, expect, it } from 'vitest'
import type { Thread, ThreadMessage } from './api'
import { formatTimestamp, isArchived, toBubbles } from './thread'

const utc = { timeZone: 'UTC', now: new Date('2026-08-14T12:00:00Z') }

function thread(messages: Array<Partial<ThreadMessage>>, extra: Partial<Thread> = {}): Thread {
  return {
    token: 'tok',
    status: 'open',
    visitor_email: 'v@example.com',
    visitor_name: 'Ada',
    created_at: '2026-08-14T09:00:00Z',
    messages: messages.map((m, i) => ({
      id: i + 1,
      sender: 'visitor',
      author: 'Ada',
      body: 'body',
      created_at: '2026-08-14T10:00:00Z',
      ...m,
    })),
    ...extra,
  }
}

describe('toBubbles', () => {
  it('places visitor and operator messages on opposite sides', () => {
    const bubbles = toBubbles(
      thread([
        { sender: 'visitor', author: 'Ada', body: 'help' },
        { sender: 'operator', author: 'Support', body: 'on it' },
      ]),
      utc,
    )

    expect(bubbles.map((b) => b.side)).toEqual(['visitor', 'support'])
    expect(bubbles.map((b) => b.author)).toEqual(['Ada', 'Support'])
    expect(bubbles[1].body).toBe('on it')
  })

  it('shows the author only when the side changes', () => {
    const bubbles = toBubbles(
      thread([
        { sender: 'visitor' },
        { sender: 'visitor' },
        { sender: 'operator' },
        { sender: 'visitor' },
      ]),
      utc,
    )
    expect(bubbles.map((b) => b.showAuthor)).toEqual([true, false, true, true])
  })

  it('falls back to sensible labels when the author is blank', () => {
    const anon = thread([{ sender: 'visitor', author: '' }, { sender: 'operator', author: '' }], {
      visitor_name: '',
    })
    expect(toBubbles(anon, utc).map((b) => b.author)).toEqual(['You', 'Support'])
  })

  it('keeps the server order and ids', () => {
    const bubbles = toBubbles(thread([{ id: 7 }, { id: 9 }]), utc)
    expect(bubbles.map((b) => b.id)).toEqual([7, 9])
  })

  it('renders an empty thread as no bubbles', () => {
    expect(toBubbles(thread([]), utc)).toEqual([])
  })
})

describe('formatTimestamp', () => {
  it('shows time only for today', () => {
    expect(formatTimestamp('2026-08-14T08:05:00Z', utc)).toBe('08:05')
  })

  it('adds the day and month within the same year', () => {
    expect(formatTimestamp('2026-03-02T08:05:00Z', utc)).toBe('2 Mar, 08:05')
  })

  it('adds the year for older messages', () => {
    expect(formatTimestamp('2025-12-31T23:30:00Z', utc)).toBe('31 Dec 2025, 23:30')
  })

  it('returns an empty string for an unparseable timestamp', () => {
    expect(formatTimestamp('not a date', utc)).toBe('')
  })

  // 2026-01-01T02:00Z is still 2025 in New York, so a June 2025 message is
  // within the same displayed year and must not be labelled with one.
  it('compares years in the requested zone, not UTC', () => {
    const newYear = { timeZone: 'America/New_York', now: new Date('2026-01-01T02:00:00Z') }
    expect(formatTimestamp('2025-06-15T12:00:00Z', newYear)).toBe('15 Jun, 08:00')
  })
})

describe('isArchived', () => {
  it.each([
    [null, false],
    [thread([]), false],
    [thread([], { status: 'archived' }), true],
  ])('%#', (t, want) => {
    expect(isArchived(t as Thread | null)).toBe(want)
  })
})
