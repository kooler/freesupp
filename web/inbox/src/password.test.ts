import { describe, expect, it } from 'vitest'
import { passwordProblem } from './password'

describe('passwordProblem', () => {
  it.each(['Password1', 'Sup3r-Secret!', 'aA1aA1aA'])('accepts %s', (pw) => {
    expect(passwordProblem(pw)).toBeNull()
  })

  it.each([
    ['', 'empty'],
    ['Pass1xy', 'too short'],
    ['password1', 'no uppercase'],
    ['PASSWORD1', 'no lowercase'],
    ['Passwordx', 'no digit'],
    ['Aa1'.repeat(25), 'over the bcrypt limit'],
  ])('rejects %s (%s)', (pw) => {
    expect(passwordProblem(pw)).toBeTruthy()
  })
})
