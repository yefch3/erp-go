import { describe, expect, it, vi } from 'vitest'

import { installStaleBundleRecovery, isStaleChunkError, shouldReloadOnce } from './staleBundle'

// 旧标签页在新版本上线后点不开新页面（2026-09-19 船期管理那件事）。这里钉
// 的是：认得出三家浏览器的措辞、不把别的错当成它、同一个地址只自动刷一次。

describe('isStaleChunkError', () => {
  it('三家浏览器的措辞都认得', () => {
    expect(isStaleChunkError(new TypeError('Failed to fetch dynamically imported module: https://x/assets/ShippingPage-abc123.js'))).toBe(true)
    expect(isStaleChunkError(new TypeError('Importing a module script failed.'))).toBe(true)
    expect(isStaleChunkError(new TypeError('error loading dynamically imported module: https://x/a.js'))).toBe(true)
  })

  it('别的错不算', () => {
    expect(isStaleChunkError(new Error('Network Error'))).toBe(false)
    expect(isStaleChunkError(new Error('Cannot read properties of undefined'))).toBe(false)
    expect(isStaleChunkError(undefined)).toBe(false)
  })
})

describe('shouldReloadOnce', () => {
  it('同一个地址：第一次刷，第二次不刷', () => {
    const store = new Map<string, string>()
    const storage = { getItem: (k: string) => store.get(k) ?? null, setItem: (k: string, v: string) => void store.set(k, v) }
    expect(shouldReloadOnce('/shipping/schedules', storage)).toBe(true)
    expect(shouldReloadOnce('/shipping/schedules', storage)).toBe(false)
    expect(shouldReloadOnce('/contracts', storage)).toBe(true)
  })
})

describe('installStaleBundleRecovery', () => {
  function fakeWindow() {
    const store = new Map<string, string>()
    const listeners: Record<string, (e: Event) => void> = {}
    const win = {
      sessionStorage: { getItem: (k: string) => store.get(k) ?? null, setItem: (k: string, v: string) => void store.set(k, v) },
      location: { assign: vi.fn(), reload: vi.fn(), pathname: '/contracts', search: '', hash: '' },
      addEventListener: (name: string, fn: (e: Event) => void) => { listeners[name] = fn },
    }
    return { win: win as unknown as Window, listeners, assign: win.location.assign, reload: win.location.reload }
  }

  it('路由跳转拿不到页面文件：整页重载到目标地址，只一次', () => {
    let onError: (err: unknown, to: { fullPath: string }) => void = () => {}
    const router = { onError: (fn: typeof onError) => { onError = fn } }
    const { win, assign } = fakeWindow()
    installStaleBundleRecovery(router as never, win)

    onError(new TypeError('Failed to fetch dynamically imported module: /assets/ShippingPage-1.js'), { fullPath: '/shipping/schedules' })
    expect(assign).toHaveBeenCalledWith('/shipping/schedules')
    onError(new TypeError('Failed to fetch dynamically imported module: /assets/ShippingPage-1.js'), { fullPath: '/shipping/schedules' })
    expect(assign).toHaveBeenCalledTimes(1)
  })

  it('不是文件拿不到的错：不动', () => {
    let onError: (err: unknown, to: { fullPath: string }) => void = () => {}
    const router = { onError: (fn: typeof onError) => { onError = fn } }
    const { win, assign } = fakeWindow()
    installStaleBundleRecovery(router as never, win)
    onError(new Error('boom'), { fullPath: '/x' })
    expect(assign).not.toHaveBeenCalled()
  })

  it('Vite 预取失败：当前页重载一次', () => {
    const router = { onError: () => {} }
    const { win, listeners, reload } = fakeWindow()
    installStaleBundleRecovery(router as never, win)
    const event = { preventDefault: vi.fn() } as unknown as Event
    listeners['vite:preloadError'](event)
    listeners['vite:preloadError'](event)
    expect(reload).toHaveBeenCalledTimes(1)
  })
})
