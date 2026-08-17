import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'

// loadTurnstile keeps module-level state, so each test needs a fresh module.
async function freshLoader() {
  vi.resetModules()
  return (await import('./turnstile')).loadTurnstile
}

// Capture the injected <script> instead of letting happy-dom try to fetch it,
// so the tests drive onload/onerror themselves.
let scripts: HTMLScriptElement[] = []

/** scripts the loader tried to inject since the last reset. */
function injected(): HTMLScriptElement[] {
  return scripts
}

beforeEach(() => {
  scripts = []
  vi.spyOn(document.head, 'appendChild').mockImplementation(((el: Node) => {
    scripts.push(el as HTMLScriptElement)
    return el
  }) as typeof document.head.appendChild)
  delete window.turnstile
})

afterEach(() => {
  vi.restoreAllMocks()
  delete window.turnstile
})

describe('loadTurnstile', () => {
  it('injects Cloudflare’s script and resolves with the API it installs', async () => {
    const loadTurnstile = await freshLoader()
    const promise = loadTurnstile()

    const script = injected()[0]!
    expect(script.src).toContain('challenges.cloudflare.com')
    expect(script.src).toContain('render=explicit')

    const api = { render: vi.fn(), reset: vi.fn() }
    window.turnstile = api
    script.onload!(new Event('load'))

    await expect(promise).resolves.toBe(api)
  })

  it('resolves immediately without injecting when the API is already present', async () => {
    const api = { render: vi.fn(), reset: vi.fn() }
    window.turnstile = api
    const loadTurnstile = await freshLoader()

    await expect(loadTurnstile()).resolves.toBe(api)
    expect(injected()).toHaveLength(0)
  })

  it('injects only one script for concurrent callers', async () => {
    const loadTurnstile = await freshLoader()
    const first = loadTurnstile()
    const second = loadTurnstile()

    expect(injected()).toHaveLength(1)

    const api = { render: vi.fn(), reset: vi.fn() }
    window.turnstile = api
    injected()[0]!.onload!(new Event('load'))

    expect(await first).toBe(api)
    expect(await second).toBe(api)
  })

  it('rejects when the script loads without installing an API', async () => {
    const loadTurnstile = await freshLoader()
    const promise = loadTurnstile()
    injected()[0]!.onload!(new Event('load'))

    await expect(promise).rejects.toThrow(/without an API/)
  })

  // A transient network failure must not poison the loader: reloading the page
  // is the only other way back, and the form would silently lose its captcha.
  it('allows a retry after a failed load', async () => {
    const loadTurnstile = await freshLoader()
    const first = loadTurnstile()
    injected()[0]!.onerror!(new Event('error'))
    await expect(first).rejects.toThrow(/could not load the captcha/)

    const second = loadTurnstile()
    expect(injected()).toHaveLength(2)

    const api = { render: vi.fn(), reset: vi.fn() }
    window.turnstile = api
    injected()[1]!.onload!(new Event('load'))
    await expect(second).resolves.toBe(api)
  })
})
