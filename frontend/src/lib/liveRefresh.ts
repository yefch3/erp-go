import { watch } from 'vue'
import { connected, onLive } from '../live'

// 「有变化才刷新」：页面不再按固定间隔重拉，而是等服务端推一句「这类东西
// 变了」再拉。
//
// 起因：询盘工作台和首页的询盘待办原来每 5 秒把整个询盘列表拉一遍，占了全站
// 一半的请求；2026-10-03 压测，每秒 100 个请求时询盘列表从 0.3 秒退化到 3 秒。
// 现在服务端在询盘被保存、提交、撤回、报价、删除之后推一句 inquiry.changed，
// 页面听到了才拉。
//
// 推送是「提示」不是「保证」，断线那段时间的推送会丢。所以留了几道兜底：
//   - 推送重新连上时补拉一次（断线期间可能漏了）；
//   - 标签页从后台切回来，期间欠着的那次补上；
//   - 两分钟都没拉成过，就主动拉一次；推送断着的时候缩到 30 秒——那时候
//     页面只能靠这个知道有变化。
//
// 拉不了的时候（人正在编辑、页面在忙、刚失败还在退避、标签页在后台）不丢：
// 记一笔「欠一次」，每 5 秒本地看一眼（不发请求），能拉了就补上。
//
// 两次拉之间至少隔 5 秒：推送是发给全公司的，改动一密集，每个开着的页面都
// 会跟着连拉。有了这个下限，最坏也只是回到原来每 5 秒轮询的量。

export const FALLBACK_MS = 120_000
export const DISCONNECTED_FALLBACK_MS = 30_000
export const DEBOUNCE_MS = 400
export const MIN_GAP_MS = 5_000
export const TICK_MS = 5_000

export interface RefreshSchedulerOptions {
  // 页面自己的重拉。返回 false 表示失败了：这次还欠着，等能拉的时候再试。
  refresh: () => Promise<boolean | void>
  // 现在适不适合拉（没在编辑、不忙、退避窗口已过）。
  canRefresh?: () => boolean
  // 标签页在不在后台。后台的页面不拉，切回来再说。
  isHidden?: () => boolean
  // 多久没拉成就主动拉一次；可以随推送连没连着变。
  fallbackMs?: number | (() => number)
  debounceMs?: number
  minGapMs?: number
}

export interface RefreshScheduler {
  // 收到一条相关推送，或者推送重新连上了。
  notify(): void
  // 本地心跳：欠着的、太久没拉的，能拉就拉。不发请求，除非真要拉。
  tick(): void
  stop(): void
}

export function createRefreshScheduler(options: RefreshSchedulerOptions): RefreshScheduler {
  const fallback = options.fallbackMs ?? FALLBACK_MS
  const fallbackMs = typeof fallback === 'function' ? fallback : () => fallback
  const debounceMs = options.debounceMs ?? DEBOUNCE_MS
  const minGapMs = options.minGapMs ?? MIN_GAP_MS
  let owed = false
  let running = false
  let stopped = false
  let lastSuccess = Date.now()
  let lastStart = -Infinity
  let timer: ReturnType<typeof setTimeout> | undefined

  const allowed = () => !stopped && !(options.isHidden?.() ?? false) && (options.canRefresh?.() ?? true)

  // 同一波推送（保存一次会同时推「询盘变了」和「采购需求变了」）合成一次拉。
  function schedule() {
    owed = true
    if (timer !== undefined || running || !allowed()) return
    // 上限 minGap：系统时钟往回拨时，别把下一次排到很久以后。
    const wait = Math.max(debounceMs, Math.min(minGapMs, lastStart + minGapMs - Date.now()))
    timer = setTimeout(() => void run(), wait)
  }

  async function run() {
    timer = undefined
    if (running || !allowed()) return // 还欠着，下一次心跳再看
    owed = false
    running = true
    lastStart = Date.now()
    let ok: boolean | void = false
    try {
      ok = await options.refresh()
    } catch {
      ok = false
    }
    running = false
    if (ok === false) {
      // 失败了不马上重来：页面的退避窗口（canRefresh）说可以了，心跳再补。
      owed = true
      return
    }
    lastSuccess = Date.now()
    if (owed) schedule() // 拉的过程中又来了推送
  }

  return {
    notify: schedule,
    tick() {
      if (owed || Date.now() - lastSuccess >= fallbackMs()) schedule()
    },
    stop() {
      stopped = true
      if (timer !== undefined) clearTimeout(timer)
      timer = undefined
    },
  }
}

export interface LiveRefresh {
  // 页面自己那一次拉失败了（比如进页面时的第一次）：记着，能拉时补上。
  owe(): void
  // 在 onUnmounted 里调用。
  stop(): void
}

// 页面用的那一层：接上推送、断线重连、标签页切换和本地心跳。
export function useLiveRefresh(types: string[], options: Omit<RefreshSchedulerOptions, 'isHidden' | 'fallbackMs'>): LiveRefresh {
  const scheduler = createRefreshScheduler({
    ...options,
    isHidden: () => document.visibilityState === 'hidden',
    fallbackMs: () => (connected.value ? FALLBACK_MS : DISCONNECTED_FALLBACK_MS),
  })
  const stopLive = onLive((event) => {
    if (types.includes(event.type)) scheduler.notify()
  })
  // 只在「断过又连上」时补拉。打开页面时推送才第一次连上，那会儿页面自己
  // 刚拉过，再拉一次是白费。
  let everUp = connected.value
  const stopReconnect = watch(connected, (up) => {
    if (up && everUp) scheduler.notify()
    if (up) everUp = true
  })
  const onVisible = () => scheduler.tick()
  document.addEventListener('visibilitychange', onVisible)
  const heartbeat = setInterval(() => scheduler.tick(), TICK_MS)
  return {
    owe: scheduler.notify,
    stop() {
      scheduler.stop()
      stopLive()
      stopReconnect()
      document.removeEventListener('visibilitychange', onVisible)
      clearInterval(heartbeat)
    },
  }
}
