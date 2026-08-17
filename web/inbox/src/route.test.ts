import { describe, expect, it } from 'vitest'
import { conversationFromPath, conversationPathFor } from './route'

describe('conversationFromPath', () => {
  it.each([
    ['/conversations/12', 12],
    ['/conversations/12/', 12],
    ['/', null],
    ['/conversations', null],
    ['/conversations/abc', null],
    ['/conversations/0', null],
    ['/conversations/12/extra', null],
  ])('%s → %s', (path, expected) => {
    expect(conversationFromPath(path)).toBe(expected)
  })
})

describe('conversationPathFor', () => {
  it('builds the deep link', () => {
    expect(conversationPathFor(12)).toBe('/conversations/12')
  })

  it('returns the inbox root when nothing is selected', () => {
    expect(conversationPathFor(null)).toBe('/')
  })
})
