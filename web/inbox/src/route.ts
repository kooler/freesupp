const conversationPath = /^\/conversations\/(\d+)\/?$/

/**
 * conversationFromPath reads the id out of the deep link operator notification
 * emails point at (`BASE_URL/conversations/{id}`).
 */
export function conversationFromPath(pathname: string): number | null {
  const m = conversationPath.exec(pathname)
  if (!m) return null
  const id = Number(m[1])
  return Number.isSafeInteger(id) && id > 0 ? id : null
}

/** conversationPathFor is the URL shown while a conversation is selected. */
export function conversationPathFor(id: number | null): string {
  return id === null ? '/' : `/conversations/${id}`
}
