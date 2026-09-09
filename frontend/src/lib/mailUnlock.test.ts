import { beforeEach, describe, expect, it } from 'vitest'
import {
  allTokens,
  clearAll,
  currentToken,
  forgetMailbox,
  saveTokens,
  searchScopeHeader,
  useMailbox,
  unlockedMailboxes,
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

  it('退出一个箱之后，它就不在「还开着」的名单里了', () => {
    saveTokens([
      { accountId: 1, email: 'a@x.com', token: 'tok-a' },
      { accountId: 2, email: 'b@x.com', token: 'tok-b' },
    ])
    expect(unlockedMailboxes().sort()).toEqual([1, 2])
    forgetMailbox(1)
    // 写信框的发件人下拉读的就是这个。1 还留在里面的话，「一个一个退出」
    // 只退了一半：读不到它的信，却还能以它的地址给客户写信。
    expect(unlockedMailboxes()).toEqual([2])
  })

  it('垃圾键不会变成一个假信箱', () => {
    localStorage.setItem('mailUnlockTokens', '{"abc":"tok","0":"tok","3":"tok"}')
    expect(unlockedMailboxes()).toEqual([3])
  })

  it('全清之后什么都不剩', () => {
    saveTokens([{ accountId: 1, email: 'a@x.com', token: 'tok-a' }])
    useMailbox(1)
    clearAll()
    expect(currentToken()).toBe('')
    expect(allTokens()).toEqual([])
  })

  // 搜索横跨信箱，范围由这个头报上去、由服务端逐把核对。
  describe('searchScopeHeader', () => {
    it('报的是手上全部的箱，不是当前这一个', () => {
      saveTokens([
        { accountId: 1, email: 'a@263.net', token: 'tok-a' },
        { accountId: 2, email: 'b@gmail.com', token: 'tok-b' },
      ])
      useMailbox(1)
      // 少报一把，那个箱就搜不到——「搜所有邮箱」这件事整个失效，而且是
      // 静静失效：结果少了几封，没有任何提示。
      expect(searchScopeHeader().split(',').sort()).toEqual(['tok-a', 'tok-b'])
    })

    it('退出过的箱不再报上去', () => {
      saveTokens([
        { accountId: 1, email: 'a@263.net', token: 'tok-a' },
        { accountId: 2, email: 'b@gmail.com', token: 'tok-b' },
      ])
      forgetMailbox(2)
      expect(searchScopeHeader()).toBe('tok-a')
    })

    it('一把都没有时是空串，不是逗号', () => {
      // 空串时请求头整个不带内容，服务端 Split 出来的是一个空片段并跳过。
      // 这里要是回 "," 或 ",,"，那边就是几次无谓的 Redis 往返。
      expect(searchScopeHeader()).toBe('')
    })

    it('封顶 32 把，和服务端的上限对齐', () => {
      // 每一把是服务端的一次 Redis 往返。上限那边也有，这里先截是为了不去
      // 发一个几 KB 的请求头。
      saveTokens(
        Array.from({ length: 40 }, (_, i) => ({
          accountId: i + 1,
          email: `a${i}@x.com`,
          token: `tok-${i}`,
        })),
      )
      expect(searchScopeHeader().split(',')).toHaveLength(32)
    })
  })
})
