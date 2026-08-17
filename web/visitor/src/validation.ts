// Mirrors the server-side caps in internal/server/public.go.
export const MESSAGE_MAX = 10000
export const FIELD_MAX = 200

// Deliberately loose: the server does the authoritative check with
// net/mail.ParseAddress, this only catches obvious typos before a round trip.
const emailish = /^[^\s@]+@[^\s@.]+(\.[^\s@.]+)+$/

export type FormValues = {
  email: string
  name: string
  message: string
}

export type FormErrors = Partial<Record<keyof FormValues, string>>

export function validateEmail(raw: string): string | undefined {
  const email = raw.trim()
  if (!email) return 'Please enter your email address.'
  if (email.length > FIELD_MAX) return 'Email address is too long.'
  if (!emailish.test(email)) return 'Please enter a valid email address.'
  return undefined
}

export function validateName(raw: string): string | undefined {
  if (raw.trim().length > FIELD_MAX) return 'Name is too long.'
  return undefined
}

export function validateMessage(raw: string): string | undefined {
  const body = raw.trim()
  if (!body) return 'Please write a message.'
  if ([...body].length > MESSAGE_MAX) return 'Message is too long.'
  return undefined
}

export function validateForm(values: FormValues): FormErrors {
  const errors: FormErrors = {}
  const email = validateEmail(values.email)
  if (email) errors.email = email
  const name = validateName(values.name)
  if (name) errors.name = name
  const message = validateMessage(values.message)
  if (message) errors.message = message
  return errors
}

export function hasErrors(errors: FormErrors): boolean {
  return Object.keys(errors).length > 0
}
