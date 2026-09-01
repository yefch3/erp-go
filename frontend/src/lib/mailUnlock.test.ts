import { beforeEach, describe, expect, it } from 'vitest'
import {
  allTokens,
  clearAll,
  currentToken,
  forgetMailbox,
  saveTokens,
  useMailbox,
} from './mailUnlock'

// 这个仓库的前端测试跑在纯 node 里（没装 jsdom / happy-dom），所以自己
// 造一个。只为一个模块引入一整套 DOM 实现不划算，而这里用到的就四个方法。
class MemoryStorage {
  private m = new Map<string, string>()
  getItem(k: string) {
    return this.m.has(k) ? (this.m.get(k) as string) : null
  }
  setItem(k: string, v: string) {
    this.m.set(k, String(v))
  }
  removeItem(k: string) {
    this.m.delete(k)
  }
  clear() {
    this.m.clear()
  }
}
;(globalThis as { localStorage?: unknown }).localStorage = new MemoryStorage()

beforeEach(() => localStorage.clear())

describe('mailUnlock', () => {
  it('退出一个信箱，别的箱还能用', () => {
    // 这条是「点了一个退出之后全部都退了」那个毛病的反面。
    saveTokens([
      { accountId: 1, email: 'me@263.net', token: 'tok-263' },
      { accountId: 2, email: 'me@gmail.com', token: 'tok-gmail' },
    ])
    expect(useMailbox(1)).toBe(true)
    expect(currentToken()).toBe('tok-263')

    forgetMailbox(1)

    expect(useMailbox(1), '退出过的箱不该还能切进去').toBe(false)
    expect(useMailbox(2), '退出 263 把 Gmail 也退了').toBe(true)
    expect(currentToken()).toBe('tok-gmail')
  })

  it('退掉当前那个箱之后，请求带的是剩下的一把，不是一把空的', () => {
    // 不换的话，退出当前箱之后页面切到别的箱，而请求还带着刚被撤掉的令牌
    // ——每一个请求都 403，界面看起来像是"另一个箱也退了"。
    saveTokens([
      { accountId: 1, email: 'a@x.com', token: 'tok-a' },
      { accountId: 2, email: 'b@x.com', token: 'tok-b' },
    ])
    useMailbox(1)
    forgetMailbox(1)
    expect(currentToken()).toBe('tok-b')
  })

  it('最后一个箱退掉就没有令牌了，登录门该出来', () => {
    saveTokens([{ accountId: 1, email: 'a@x.com', token: 'tok-a' }])
    useMailbox(1)
    forgetMailbox(1)
    expect(currentToken()).toBe('')
  })

  it('再验证一次是合并，不是覆盖', () => {
    // 一个人可能在两个箱之间反复验证，而每次验证只回当时绑着的那些箱。
    // 覆盖的话，前一次拿到的令牌会被抹掉，那个箱就要重新输密码。
    saveTokens([{ accountId: 1, email: 'a@x.com', token: 'tok-a' }])
    saveTokens([{ accountId: 2, email: 'b@x.com', token: 'tok-b' }])
    expect(useMailbox(1)).toBe(true)
    expect(useMailbox(2)).toBe(true)
  })

  it('「全部退出」要把手上每一把都报上去，包括旧版本留下的那把', () => {
    saveTokens([
      { accountId: 1, email: 'a@x.com', token: 'tok-a' },
      { accountId: 2, email: 'b@x.com', token: 'tok-b' },
    ])
    // 换版本之前留下的：只有 mailUnlock，不在表里。漏报的话它会一直活到
    // 过期——而「全部退出」的用处正是共用电脑走人时立刻断干净。
    localStorage.setItem('mailUnlock', 'legacy-tok')
    const all = allTokens()
    expect(all).toContain('tok-a')
    expect(all).toContain('tok-b')
    expect(all).toContain('legacy-tok')
  })

  it('localStorage 里被人塞了垃圾也不炸', () => {
    localStorage.setItem('mailUnlockTokens', 'not json at all')
    expect(useMailbox(1)).toBe(false)
    expect(allTokens()).toEqual([])
    localStorage.setItem('mailUnlockTokens', '[1,2,3]')
    expect(useMailbox(1)).toBe(false)
  })

  it('全清之后什么都不剩', () => {
    saveTokens([{ accountId: 1, email: 'a@x.com', token: 'tok-a' }])
    useMailbox(1)
    clearAll()
    expect(currentToken()).toBe('')
    expect(allTokens()).toEqual([])
  })
})
