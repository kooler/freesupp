import { readFileSync } from 'node:fs'
import { resolve } from 'node:path'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'

// widget.js has no build step and is loaded raw by every host page, so it is
// exercised here as source evaluated into a document rather than imported.
const source = readFileSync(resolve(import.meta.dirname, '../../widget/widget.js'), 'utf8')
const css = readFileSync(resolve(import.meta.dirname, 'styles.css'), 'utf8')

type WidgetWindow = Window & { __freesuppWidget?: boolean }

/** stubScheme pins prefers-color-scheme and returns a trigger for a change. */
function stubScheme(dark: boolean) {
  const listeners: Array<() => void> = []
  const list = {
    matches: dark,
    addEventListener: (_: string, fn: () => void) => listeners.push(fn),
    removeEventListener: () => {},
  }
  vi.stubGlobal('matchMedia', () => list)
  return {
    set(value: boolean) {
      list.matches = value
      for (const fn of listeners) fn()
    },
  }
}

const hex = (v: string) => v.trim().replace(/^#([0-9a-f])([0-9a-f])([0-9a-f])$/i, '#$1$1$2$2$3$3')

/**
 * run evaluates widget.js with document.currentScript stubbed to a script tag
 * carrying the given attributes, then returns the elements it injected.
 */
function run(attrs: Record<string, string> = {}) {
  const script = document.createElement('script')
  script.src = attrs.src ?? 'https://support.example.com/widget.js'
  for (const [k, v] of Object.entries(attrs)) {
    if (k !== 'src') script.setAttribute(k, v)
  }
  Object.defineProperty(document, 'currentScript', { value: script, configurable: true })

  // eslint-disable-next-line no-new-func
  new Function(source)()

  return {
    root: document.querySelector('[data-freesupp="root"]') as HTMLElement | null,
    button: document.querySelector('[data-freesupp="button"]') as HTMLButtonElement | null,
    panel: document.querySelector('[data-freesupp="panel"]') as HTMLElement | null,
    frame: document.querySelector('iframe') as HTMLIFrameElement | null,
    mark: document.querySelector('[data-freesupp="button"] img') as HTMLImageElement | null,
  }
}

// Assigning iframe.src makes happy-dom navigate the frame. Record the
// assignment on the element instead, so the tests observe the URL the widget
// chose without any page loading.
const realSrc = Object.getOwnPropertyDescriptor(HTMLIFrameElement.prototype, 'src')
const assigned = new WeakMap<HTMLIFrameElement, string>()

beforeEach(() => {
  document.body.innerHTML = ''
  delete (window as WidgetWindow).__freesuppWidget
  Object.defineProperty(HTMLIFrameElement.prototype, 'src', {
    configurable: true,
    get(this: HTMLIFrameElement) {
      return assigned.get(this) ?? ''
    },
    set(this: HTMLIFrameElement, value: string) {
      assigned.set(this, value)
    },
  })
})

afterEach(() => {
  if (realSrc) Object.defineProperty(HTMLIFrameElement.prototype, 'src', realSrc)
  vi.restoreAllMocks()
})

describe('widget.js', () => {
  it('derives the panel URL from its own src', async () => {
    const w = run({ src: 'https://support.example.com/widget.js?v=2#frag' })
    w.button!.click()
    expect(w.frame!.src).toBe('https://support.example.com/widget/')
  })

  it('keeps a path prefix when the script is served from a subdirectory', () => {
    const w = run({ src: 'https://example.com/support/widget.js' })
    w.button!.click()
    expect(w.frame!.src).toBe('https://example.com/support/widget/')
  })

  it('prefers data-base-url when the script is served from a CDN', () => {
    const w = run({
      src: 'https://cdn.example.net/assets/widget.js',
      'data-base-url': 'https://support.example.com',
    })
    w.button!.click()
    expect(w.frame!.src).toBe('https://support.example.com/widget/')
  })

  it('applies the label and colour attributes', () => {
    const w = run({ 'data-label': 'Need help?', 'data-color': '#111827' })
    expect(w.button!.getAttribute('aria-label')).toBe('Need help?')
    expect(w.button!.style.background).toBe('#111827')
  })

  it('defaults the label when no attribute is given', () => {
    const w = run()
    expect(w.button!.getAttribute('aria-label')).toBe('Contact support')
  })

  it('loads the iframe lazily and toggles the panel', () => {
    const w = run()
    expect(w.frame!.src).toBe('')
    expect(w.button!.getAttribute('aria-expanded')).toBe('false')

    w.button!.click()
    expect(w.frame!.src).toContain('/widget/')
    expect(w.button!.getAttribute('aria-expanded')).toBe('true')
    expect(w.panel!.style.display).toBe('block')

    w.button!.click()
    expect(w.button!.getAttribute('aria-expanded')).toBe('false')
    expect(w.panel!.style.opacity).toBe('0')
  })

  it('shows the logo mark from the same deployment as the script', () => {
    const w = run({ src: 'https://example.com/support/widget.js' })
    expect(w.mark!.getAttribute('src')).toBe('https://example.com/support/widget-mark.png')
    expect(w.mark!.getAttribute('aria-hidden')).toBe('true')
    expect(w.mark!.alt).toBe('')
  })

  it('swaps the mark for a close icon while the panel is open', () => {
    const w = run()
    const close = w.button!.querySelector('svg') as SVGElement

    expect(w.mark!.style.display).toBe('block')
    expect(close.style.display).toBe('none')

    w.button!.click()
    expect(w.mark!.style.display).toBe('none')
    expect(close.style.display).toBe('block')
  })

  it('paints the panel dark when the OS asks for it', () => {
    stubScheme(true)
    const w = run()
    expect(w.root!.style.getPropertyValue('color-scheme')).toBe('dark')
    expect(w.panel!.style.background).toBe('#0f172a')
    expect(w.frame!.style.background).toBe('#0f172a')
  })

  it('repaints when the OS scheme changes', () => {
    const scheme = stubScheme(true)
    const w = run()
    scheme.set(false)
    expect(w.root!.style.getPropertyValue('color-scheme')).toBe('light')
    expect(w.panel!.style.background).toBe('#ffffff')
  })

  // widget.js paints the panel behind the visitor document while it loads, so
  // its two backgrounds have to be the app's --fs-bg for the same scheme.
  it('keeps the panel background in step with the visitor palette', () => {
    const light = /:root\s*\{[^}]*--fs-bg:\s*([^;]+);/.exec(css)
    const dark = /prefers-color-scheme: dark\)[^{]*\{\s*:root\s*\{[^}]*--fs-bg:\s*([^;]+);/.exec(css)
    expect(light, 'no light --fs-bg in styles.css').not.toBeNull()
    expect(dark, 'no dark --fs-bg in styles.css').not.toBeNull()

    expect(source).toContain("light: { bg: '" + hex(light![1]) + "'")
    expect(source).toContain("dark: { bg: '" + hex(dark![1]) + "'")
  })

  it('does not inject a second widget when evaluated twice', () => {
    run()
    run()
    expect(document.querySelectorAll('[data-freesupp="root"]')).toHaveLength(1)
  })

  // The visitor app asks the host page to close the panel after a successful
  // submission. Accepting that message from any origin would let any frame on
  // the page drive the widget.
  it('closes on a close message from the panel origin', () => {
    const w = run()
    w.button!.click()
    expect(w.button!.getAttribute('aria-expanded')).toBe('true')

    window.dispatchEvent(
      new MessageEvent('message', {
        origin: 'https://support.example.com',
        data: { source: 'freesupp', type: 'close' },
      }),
    )
    expect(w.button!.getAttribute('aria-expanded')).toBe('false')
  })

  it('ignores close messages from any other origin', () => {
    const w = run()
    w.button!.click()

    for (const data of [
      { origin: 'https://evil.example.com', data: { source: 'freesupp', type: 'close' } },
      { origin: 'https://support.example.com', data: { source: 'other', type: 'close' } },
      { origin: 'https://support.example.com', data: { source: 'freesupp', type: 'resize' } },
    ]) {
      window.dispatchEvent(new MessageEvent('message', data))
      expect(w.button!.getAttribute('aria-expanded')).toBe('true')
    }
  })
})
