import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'

import { DEBOUNCE_MS, FALLBACK_MS, MIN_GAP_MS, createRefreshScheduler } from './liveRefresh'

describe('createRefreshScheduler', () => {
  beforeEach(() => vi.useFakeTimers())
  afterEach(() => vi.useRealTimers())

  function setup(overrides: { ok?: boolean[]; canRefresh?: () => boolean; isHidden?: () => boolean; fallbackMs?: () => number } = {}) {
    const results = [...(overrides.ok ?? [])]
    const refresh = vi.fn(async () => results.shift() ?? true)
    const scheduler = createRefreshScheduler({ refresh, canRefresh: overrides.canRefresh, isHidden: overrides.isHidden, fallbackMs: overrides.fallbackMs })
    return { refresh, scheduler }
  }

  it('一波推送只拉一次', async () => {
    const { refresh, scheduler } = setup()
    scheduler.notify()
    scheduler.notify()
    scheduler.notify()
    await vi.advanceTimersByTimeAsync(DEBOUNCE_MS)
    expect(refresh).toHaveBeenCalledTimes(1)
  })

  it('没有推送就不拉，直到两分钟兜底', async () => {
    const { refresh, scheduler } = setup()
    for (let i = 0; i < 23; i++) {
      await vi.advanceTimersByTimeAsync(5_000)
      scheduler.tick()
    }
    expect(refresh).not.toHaveBeenCalled()
    await vi.advanceTimersByTimeAsync(FALLBACK_MS - 23 * 5_000)
    scheduler.tick()
    await vi.advanceTimersByTimeAsync(DEBOUNCE_MS)
    expect(refresh).toHaveBeenCalledTimes(1)
  })

  it('正在编辑时欠着，编辑完心跳补上', async () => {
    let editing = true
    const { refresh, scheduler } = setup({ canRefresh: () => !editing })
    scheduler.notify()
    await vi.advanceTimersByTimeAsync(10_000)
    scheduler.tick()
    expect(refresh).not.toHaveBeenCalled()
    editing = false
    scheduler.tick()
    await vi.advanceTimersByTimeAsync(DEBOUNCE_MS)
    expect(refresh).toHaveBeenCalledTimes(1)
    scheduler.tick()
    await vi.advanceTimersByTimeAsync(DEBOUNCE_MS)
    expect(refresh).toHaveBeenCalledTimes(1)
  })

  it('后台标签页不拉，切回来补一次', async () => {
    let hidden = true
    const { refresh, scheduler } = setup({ isHidden: () => hidden })
    scheduler.notify()
    scheduler.notify()
    await vi.advanceTimersByTimeAsync(30_000)
    scheduler.tick()
    expect(refresh).not.toHaveBeenCalled()
    hidden = false
    scheduler.tick()
    await vi.advanceTimersByTimeAsync(DEBOUNCE_MS)
    expect(refresh).toHaveBeenCalledTimes(1)
  })

  it('失败了还欠着，退避窗口过了再试，成功后不再重复', async () => {
    let backingOff = false
    const { refresh, scheduler } = setup({ ok: [false, true], canRefresh: () => !backingOff })
    scheduler.notify()
    await vi.advanceTimersByTimeAsync(DEBOUNCE_MS)
    expect(refresh).toHaveBeenCalledTimes(1)
    backingOff = true
    scheduler.tick()
    await vi.advanceTimersByTimeAsync(DEBOUNCE_MS)
    expect(refresh).toHaveBeenCalledTimes(1)
    backingOff = false
    scheduler.tick()
    await vi.advanceTimersByTimeAsync(MIN_GAP_MS)
    expect(refresh).toHaveBeenCalledTimes(2)
    scheduler.tick()
    await vi.advanceTimersByTimeAsync(MIN_GAP_MS)
    expect(refresh).toHaveBeenCalledTimes(2)
  })

  it('拉的过程中又来推送，拉完再拉一次', async () => {
    let finish: (ok: boolean) => void = () => {}
    const refresh = vi.fn(() => new Promise<boolean>((resolve) => { finish = resolve }))
    const scheduler = createRefreshScheduler({ refresh })
    scheduler.notify()
    await vi.advanceTimersByTimeAsync(DEBOUNCE_MS)
    expect(refresh).toHaveBeenCalledTimes(1)
    scheduler.notify()
    await vi.advanceTimersByTimeAsync(DEBOUNCE_MS)
    expect(refresh).toHaveBeenCalledTimes(1)
    finish(true)
    await vi.advanceTimersByTimeAsync(MIN_GAP_MS)
    expect(refresh).toHaveBeenCalledTimes(2)
  })

  it('推送再密，两次拉之间也至少隔 5 秒', async () => {
    const { refresh, scheduler } = setup()
    for (let elapsed = 0; elapsed < 20_000; elapsed += 100) {
      scheduler.notify()
      await vi.advanceTimersByTimeAsync(100)
    }
    await vi.advanceTimersByTimeAsync(MIN_GAP_MS)
    // 20 秒的连续推送：第一次 + 之后每 5 秒一次，再加收尾那一次。
    expect(refresh.mock.calls.length).toBeGreaterThanOrEqual(4)
    expect(refresh.mock.calls.length).toBeLessThanOrEqual(6)
  })

  it('推送断着的时候兜底缩短', async () => {
    let up = false
    const { refresh, scheduler } = setup({ fallbackMs: () => (up ? FALLBACK_MS : 30_000) })
    await vi.advanceTimersByTimeAsync(30_000)
    scheduler.tick()
    await vi.advanceTimersByTimeAsync(DEBOUNCE_MS)
    expect(refresh).toHaveBeenCalledTimes(1)
    up = true
    await vi.advanceTimersByTimeAsync(30_000)
    scheduler.tick()
    await vi.advanceTimersByTimeAsync(MIN_GAP_MS)
    expect(refresh).toHaveBeenCalledTimes(1)
  })

  it('停了之后什么都不做', async () => {
    const { refresh, scheduler } = setup()
    scheduler.notify()
    scheduler.stop()
    await vi.advanceTimersByTimeAsync(FALLBACK_MS)
    scheduler.tick()
    scheduler.notify()
    await vi.advanceTimersByTimeAsync(DEBOUNCE_MS)
    expect(refresh).not.toHaveBeenCalled()
  })
})
