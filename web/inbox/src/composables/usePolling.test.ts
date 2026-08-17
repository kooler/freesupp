import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { usePolling } from './usePolling'

beforeEach(() => {
  vi.useFakeTimers()
})

afterEach(() => {
  vi.useRealTimers()
  vi.unstubAllGlobals()
})

describe('usePolling', () => {
  it('runs the task on the interval, not before start', async () => {
    const task = vi.fn()
    const polling = usePolling(task, { intervalMs: 20_000 })

    await vi.advanceTimersByTimeAsync(60_000)
    expect(task).not.toHaveBeenCalled()

    polling.start()
    await vi.advanceTimersByTimeAsync(20_000)
    expect(task).toHaveBeenCalledTimes(1)
    await vi.advanceTimersByTimeAsync(40_000)
    expect(task).toHaveBeenCalledTimes(3)

    polling.stop()
    await vi.advanceTimersByTimeAsync(60_000)
    expect(task).toHaveBeenCalledTimes(3)
  })

  it('is idempotent — a second start does not double the rate', () => {
    const task = vi.fn()
    const polling = usePolling(task, { intervalMs: 20_000 })

    polling.start()
    polling.start()
    vi.advanceTimersByTime(20_000)
    expect(task).toHaveBeenCalledTimes(1)
    polling.stop()
  })

  it('drops a tick while the previous run is still in flight', async () => {
    let release: (() => void) | null = null
    const task = vi.fn(() => new Promise<void>((resolve) => (release = resolve)))
    const polling = usePolling(task, { intervalMs: 20_000 })

    polling.start()
    await vi.advanceTimersByTimeAsync(20_000)
    expect(task).toHaveBeenCalledTimes(1)

    await vi.advanceTimersByTimeAsync(40_000)
    expect(task).toHaveBeenCalledTimes(1)

    release!()
    await vi.advanceTimersByTimeAsync(20_000)
    expect(task).toHaveBeenCalledTimes(2)
    polling.stop()
  })

  it('keeps polling after the task rejects', async () => {
    const task = vi.fn(async () => {
      throw new Error('offline')
    })
    const polling = usePolling(task, { intervalMs: 20_000 })

    polling.start()
    await vi.advanceTimersByTimeAsync(20_000)
    expect(task).toHaveBeenCalledTimes(1)
    await vi.advanceTimersByTimeAsync(20_000)
    expect(task).toHaveBeenCalledTimes(2)
    polling.stop()
  })

  it('skips interval runs while the tab is hidden', () => {
    const task = vi.fn()
    const polling = usePolling(task, { intervalMs: 20_000 })
    const hidden = vi.spyOn(document, 'hidden', 'get').mockReturnValue(true)

    polling.start()
    vi.advanceTimersByTime(40_000)
    expect(task).not.toHaveBeenCalled()

    hidden.mockReturnValue(false)
    vi.advanceTimersByTime(20_000)
    expect(task).toHaveBeenCalledTimes(1)
    polling.stop()
    hidden.mockRestore()
  })

  it('runs on window focus and stops listening once stopped', () => {
    const task = vi.fn()
    const polling = usePolling(task, { intervalMs: 0 })

    polling.start()
    vi.advanceTimersByTime(120_000)
    expect(task).not.toHaveBeenCalled() // focus-only: no timer

    window.dispatchEvent(new Event('focus'))
    expect(task).toHaveBeenCalledTimes(1)

    polling.stop()
    window.dispatchEvent(new Event('focus'))
    expect(task).toHaveBeenCalledTimes(1)
  })

})
