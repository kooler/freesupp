/**
 * passwordProblem mirrors the server's complexity rule so forms can flag a
 * weak password before submitting. The server remains the authority.
 */
export function passwordProblem(password: string): string | null {
  if (password.length > 72) {
    return 'Password must be at most 72 characters.'
  }
  if (
    password.length < 8 ||
    !/[A-Z]/.test(password) ||
    !/[a-z]/.test(password) ||
    !/[0-9]/.test(password)
  ) {
    return 'Password must be at least 8 characters and contain an uppercase letter, a lowercase letter and a digit.'
  }
  return null
}
