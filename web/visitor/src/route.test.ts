import { describe, expect, it } from 'vitest'
import { resolveRoute } from './route'

describe('resolveRoute', () => {
  it('maps the magic-link path to the thread page', () => {
    expect(resolveRoute('/t/abc123')).toEqual({ name: 'thread', token: 'abc123' })
  })

  it('tolerates a trailing slash', () => {
    expect(resolveRoute('/t/abc123/')).toEqual({ name: 'thread', token: 'abc123' })
  })

  it('decodes escaped tokens', () => {
    expect(resolveRoute('/t/a%20b')).toEqual({ name: 'thread', token: 'a b' })
  })

  it('keeps a malformed escape sequence as-is', () => {
    expect(resolveRoute('/t/a%zz')).toEqual({ name: 'thread', token: 'a%zz' })
  })

  it.each(['/widget/', '/', '/visitor/', '/t/', '/t/a/b', '/nope'])(
    'falls back to the form for %s',
    (path) => {
      expect(resolveRoute(path)).toEqual({ name: 'widget' })
    },
  )
})
