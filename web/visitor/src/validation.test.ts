import { describe, expect, it } from 'vitest'
import {
  FIELD_MAX,
  MESSAGE_MAX,
  hasErrors,
  validateEmail,
  validateForm,
  validateMessage,
  validateName,
} from './validation'

describe('validateEmail', () => {
  const cases: Array<[string, string, boolean]> = [
    ['plain address', 'visitor@example.com', true],
    ['surrounding space', '  visitor@example.com  ', true],
    ['subdomain', 'a.b@mail.example.co.uk', true],
    ['plus tag', 'visitor+tag@example.com', true],
    ['empty', '', false],
    ['whitespace only', '   ', false],
    ['no at sign', 'visitor.example.com', false],
    ['no domain dot', 'visitor@example', false],
    ['inner space', 'vis itor@example.com', false],
    ['display name form', 'Visitor <v@example.com>', false],
    ['too long', `${'a'.repeat(FIELD_MAX)}@example.com`, false],
  ]

  it.each(cases)('%s', (_name, input, ok) => {
    expect(validateEmail(input) === undefined).toBe(ok)
  })
})

describe('validateMessage', () => {
  it('accepts a normal body', () => {
    expect(validateMessage('my printer is on fire')).toBeUndefined()
  })

  it('rejects blank bodies', () => {
    expect(validateMessage('   \n ')).toMatch(/write a message/i)
  })

  it('rejects bodies over the server cap', () => {
    expect(validateMessage('x'.repeat(MESSAGE_MAX))).toBeUndefined()
    expect(validateMessage('x'.repeat(MESSAGE_MAX + 1))).toMatch(/too long/i)
  })

  it('counts runes, not UTF-16 units', () => {
    // Each emoji is two UTF-16 units but one rune, matching Go's []rune cap.
    expect(validateMessage('😀'.repeat(MESSAGE_MAX))).toBeUndefined()
  })
})

describe('validateName', () => {
  it('accepts an empty name', () => {
    expect(validateName('')).toBeUndefined()
  })

  it('rejects an over-long name', () => {
    expect(validateName('n'.repeat(FIELD_MAX + 1))).toMatch(/too long/i)
  })
})

describe('validateForm', () => {
  it('returns no errors for a valid submission', () => {
    const errors = validateForm({ email: 'v@example.com', name: '', message: 'hello' })
    expect(errors).toEqual({})
    expect(hasErrors(errors)).toBe(false)
  })

  it('collects every field error at once', () => {
    const errors = validateForm({ email: 'nope', name: 'n'.repeat(FIELD_MAX + 1), message: ' ' })
    expect(Object.keys(errors).sort()).toEqual(['email', 'message', 'name'])
    expect(hasErrors(errors)).toBe(true)
  })
})
