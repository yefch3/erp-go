// 邮箱解锁令牌：**一个信箱一把**。
//
// 从前是一个人一把，存在 localStorage.mailUnlock 里。那时一个人只有一个箱，
// 所以「退出邮箱」这个按钮只可能是全退——手上就那一把，撤了就什么都没了。
//
// 现在验证成功时服务端会把这个人**每个箱**的令牌都发下来，浏览器全存着：
//
//   · 切换箱 = 换一把令牌，不用重新输密码
//   · 退出 = 只撤当前这一把，别的箱照开
//
// mailUnlock 这个键**保留**，它始终是「此刻要发出去的那一把」——请求拦截器
// （api.ts）读的就是它，一行都不用改。新增的 mailUnlockTokens 是那张
// 信箱→令牌的表。

/** 服务端验证成功后回来的一条。 */
export interface MintedToken {
  accountId: number
  email: string
  token: string
}

const CURRENT = 'mailUnlock'
const MAP = 'mailUnlockTokens'

/** 登出 ERP 时要一起清掉的键。api.ts 用它，免得两处各记一份而漏掉新的。 */
export const UNLOCK_STORAGE_KEYS = [CURRENT, MAP] as const

function readMap(): Record<string, string> {
  try {
    const raw = localStorage.getItem(MAP)
    if (!raw) return {}
    const v = JSON.parse(raw) as unknown
    // 存进去的是我们自己写的东西，但它在 localStorage 里，谁都能改。
    // 解出来不是个对象就当没有——比让后面每一处都防一遍强。
    return v && typeof v === 'object' && !Array.isArray(v) ? (v as Record<string, string>) : {}
  } catch {
    return {}
  }
}

function writeMap(m: Record<string, string>) {
  localStorage.setItem(MAP, JSON.stringify(m))
}

/**
 * 记下服务端刚发下来的那一批令牌。
 *
 * 是**合并**不是覆盖：一个人可能在两个箱之间反复验证，后一次验证只会回
 * 当时还绑着的那些箱，覆盖会把前一次拿到的抹掉。
 */
export function saveTokens(list: MintedToken[]) {
  if (!list?.length) return
  const m = readMap()
  for (const t of list) {
    if (t?.accountId && t.token) m[String(t.accountId)] = t.token
  }
  writeMap(m)
}

/**
 * 切到这个信箱：把它那把令牌设成「此刻要发的那一把」。
 *
 * 返回 false 表示手上没有这个箱的令牌——刚退出过它，或者它是验证之后才
 * 绑上的。调用方据此把登录门重新摆出来。
 */
export function useMailbox(accountId: number): boolean {
  const tok = readMap()[String(accountId)]
  if (!tok) return false
  localStorage.setItem(CURRENT, tok)
  return true
}

/** 此刻要发出去的那一把。没有就是空串。 */
export function currentToken(): string {
  return localStorage.getItem(CURRENT) ?? ''
}

/**
 * 忘掉一个信箱的令牌——退出它之后调用。
 *
 * 顺带把 mailUnlock 换成剩下的任意一把：退出当前箱之后页面会切到别的箱，
 * 那时请求得带着能用的令牌。一把都不剩就清空，登录门自己会出来。
 */
export function forgetMailbox(accountId: number) {
  const m = readMap()
  const gone = m[String(accountId)]
  delete m[String(accountId)]
  writeMap(m)
  if (gone && currentToken() === gone) {
    const rest = Object.values(m)
    if (rest.length) localStorage.setItem(CURRENT, rest[0])
    else localStorage.removeItem(CURRENT)
  }
}

/**
 * 手上还开着的那些信箱。
 *
 * 写信框的发件人下拉用它筛：退出了 163 之后，163 不该还留在下拉里。留着的
 * 话「一个一个退出」只退了一半——读不到它的信，却还能以它的地址给客户写信。
 *
 * 服务端只能验「请求带的这把令牌开的是不是这个箱」，一个请求只带一把，所以
 * 这条口径落在浏览器这边。它挡的是日常路径，不是攻击者。
 */
export function unlockedMailboxes(): number[] {
  return Object.keys(readMap())
    .map(Number)
    .filter((n) => Number.isFinite(n) && n > 0)
}

/** 手上所有令牌，「全部退出」要把它们一起报给服务端撤掉。 */
export function allTokens(): string[] {
  const m = readMap()
  const cur = currentToken()
  const out = Object.values(m)
  // 当前那把可能是旧版本留下的、不在表里——一起报上去，不然它会活到过期。
  if (cur && !out.includes(cur)) out.push(cur)
  return out
}

/** 全清。登出 ERP 和「全部退出」用。 */
export function clearAll() {
  localStorage.removeItem(CURRENT)
  localStorage.removeItem(MAP)
}
