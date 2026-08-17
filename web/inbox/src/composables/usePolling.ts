import { onScopeDispose } from 'vue'

export type PollingOptions = {
  /** milliseconds between runs; 0 or less means focus-only, no timer. */
  intervalMs: number
  /** also run when the tab regains focus (default true). */
  onFocus?: boolean
}

export type Polling = {
  start: () => void
  stop: () => void
}

/**
 * usePolling repeats a task on an interval and/or whenever the tab regains
 * focus. Runs never overlap: a tick while the previous run is still in flight
 * is dropped rather than queued, so a slow network cannot pile up requests.
 */
export function usePolling(task: () => unknown | Promise<unknown>, opts: PollingOptions): Polling {
  const onFocus = opts.onFocus ?? true

  let timer: ReturnType<typeof setInterval> | null = null
  let started = false
  let running = false

  const run = async () => {
    if (running) return
    running = true
    try {
      await task()
    } catch {
      // A failed poll is not fatal — the next run tries again.
    } finally {
      running = false
    }
  }

  // A hidden tab does not need fresh data; the focus handler catches it up.
  const tick = () => {
    if (typeof document !== 'undefined' && document.hidden) return
    void run()
  }

  const focused = () => {
    void run()
  }

  const startTimer = () => {
    if (opts.intervalMs > 0) timer = setInterval(tick, opts.intervalMs)
  }

  const clearTimer = () => {
    if (timer !== null) {
      clearInterval(timer)
      timer = null
    }
  }

  const start = () => {
    if (started) return
    started = true
    startTimer()
    if (onFocus && typeof window !== 'undefined') window.addEventListener('focus', focused)
  }

  const stop = () => {
    if (!started) return
    started = false
    clearTimer()
    if (onFocus && typeof window !== 'undefined') window.removeEventListener('focus', focused)
  }

  onScopeDispose(stop, true)

  return { start, stop }
}
