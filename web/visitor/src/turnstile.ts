const SCRIPT_URL = 'https://challenges.cloudflare.com/turnstile/v0/api.js?render=explicit'

type TurnstileAPI = {
  render: (el: HTMLElement, opts: Record<string, unknown>) => string
  reset: (id?: string) => void
}

declare global {
  interface Window {
    turnstile?: TurnstileAPI
  }
}

let loading: Promise<TurnstileAPI> | null = null

/** loadTurnstile injects Cloudflare's script once and resolves with its API. */
export function loadTurnstile(): Promise<TurnstileAPI> {
  if (window.turnstile) return Promise.resolve(window.turnstile)
  if (loading) return loading

  loading = new Promise<TurnstileAPI>((resolve, reject) => {
    const el = document.createElement('script')
    el.src = SCRIPT_URL
    el.async = true
    el.defer = true
    el.onload = () => {
      if (window.turnstile) resolve(window.turnstile)
      else reject(new Error('turnstile script loaded without an API'))
    }
    el.onerror = () => reject(new Error('could not load the captcha'))
    document.head.appendChild(el)
  })
  loading.catch(() => {
    loading = null // allow a retry after a transient network failure
  })
  return loading
}
