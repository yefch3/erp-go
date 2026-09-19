// 旧标签页在新版本上线后「点了进不去」——这里让它自己刷新一次。
//
// 页面是按路由懒加载的，每次上线文件名的哈希都会变。一个上线前就开着、
// 一直没刷新的标签页，点一个还没打开过的页面时，去拿的是旧文件名，服务器
// 上已经没有了：浏览器报"加载模块失败"，路由跳转静默作废，人看到的就是
// "点了没反应"。2026-09-19 老板端的船期管理就是这么"进不去"的——账号、
// 权限、接口全没问题，日志里也没有任何错。
//
// 处理办法是业界通行的：认出这一类错误，整页重新加载到目标地址，新版本的
// 文件就都在了。每个地址只自动重载一次（记在 sessionStorage 里），万一真是
// 别的原因加载不到，不会陷入无限刷新。

import type { Router } from 'vue-router'

/** 各家浏览器对"动态 import 的文件拿不到"的措辞。 */
const STALE_CHUNK_PATTERNS = [
  /failed to fetch dynamically imported module/i, // Chrome / Edge
  /importing a module script failed/i, // Safari
  /error loading dynamically imported module/i, // Firefox
  /loading (css )?chunk \S+ failed/i, // 老 webpack 风格的措辞，留着无害
]

export function isStaleChunkError(err: unknown): boolean {
  const text = err instanceof Error ? `${err.name}: ${err.message}` : String(err ?? '')
  return STALE_CHUNK_PATTERNS.some((re) => re.test(text))
}

const RELOADED_KEY_PREFIX = 'stale-bundle-reloaded:'

/**
 * 这个地址是不是第一次因为文件拿不到而要求重载。是就记一笔并返回 true；
 * 已经重载过一次还失败，返回 false——那不是版本旧，别再刷了。
 */
export function shouldReloadOnce(path: string, storage: Pick<Storage, 'getItem' | 'setItem'>): boolean {
  const key = RELOADED_KEY_PREFIX + path
  if (storage.getItem(key)) return false
  storage.setItem(key, String(Date.now()))
  return true
}

/**
 * 装到路由和窗口上：路由跳转时拿不到页面文件、或 Vite 预取文件失败，都
 * 走同一条路——整页重载到目标地址。
 */
export function installStaleBundleRecovery(router: Router, win: Window = window): void {
  router.onError((err, to) => {
    if (!isStaleChunkError(err)) return
    if (!shouldReloadOnce(to.fullPath, win.sessionStorage)) return
    win.location.assign(to.fullPath)
  })
  // Vite 在 <link rel="modulepreload"> 拿不到文件时抛这个事件；preventDefault
  // 让它不再把错误往控制台扔，由我们来重载。
  win.addEventListener('vite:preloadError', (event) => {
    event.preventDefault()
    const path = win.location.pathname + win.location.search + win.location.hash
    if (!shouldReloadOnce(path, win.sessionStorage)) return
    win.location.reload()
  })
}
