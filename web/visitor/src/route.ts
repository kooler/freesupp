export type Route = { name: 'widget' } | { name: 'thread'; token: string }

const threadPath = /^\/t\/([^/?#]+)\/?$/

/**
 * resolveRoute maps a pathname onto the two visitor screens. Anything that is
 * not a magic link is the contact form, so the app also works when Vite serves
 * it from its own base during development.
 */
export function resolveRoute(pathname: string): Route {
  const m = threadPath.exec(pathname)
  if (!m) return { name: 'widget' }
  let token = m[1]
  try {
    token = decodeURIComponent(token)
  } catch {
    // Leave a malformed escape sequence as-is; the API rejects it anyway.
  }
  return { name: 'thread', token }
}
