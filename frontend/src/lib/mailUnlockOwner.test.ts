import { beforeEach, describe, expect, it } from 'vitest'
import { readFileSync } from 'node:fs'
import { resolve } from 'node:path'
import {
  adoptVerification,
  allTokens,
  claimUnstampedTokens,
  currentToken,
  keepTokensFor,
  saveTokens,
  unlockedMailboxes,
  useMailbox,
} from './mailUnlock'

// 「授权码输一次就一直有效」那条（4 号公司 1030 建议，总经理部门）。
//
// 从前 ERP 登录一过期，浏览器就把邮箱令牌一起清掉——服务端那把还活着，
// 人却得重输授权码。现在令牌跟人走：同一个人重新登录原样留着，换了人才清。

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

// ERP 登录态过期时 api.ts 清掉的那几个键，照原样模拟。
function erpSessionExpires() {
  for (const k of ['employeeId', 'employeeName', 'employeeEmail', 'permissions']) {
    localStorage.removeItem(k)
  }
}

function signedInAs(employeeId: string) {
  localStorage.setItem('employeeId', employeeId)
}

beforeEach(() => localStorage.clear())

describe('邮箱令牌跟人走，不跟 ERP 登录态走', () => {
  it('ERP 登录过期后同一个人重新登录，不用再输授权码', () => {
    signedInAs('17')
    saveTokens([
      { accountId: 58, email: 'me@163.com', token: 'tok-163' },
      { accountId: 59, email: 'me@263.net', token: 'tok-263' },
    ])
    useMailbox(58)

    erpSessionExpires()
    keepTokensFor('17')

    expect(currentToken()).toBe('tok-163')
    expect(unlockedMailboxes().sort()).toEqual([58, 59])
  })

  it('换了一个人登录，上一个人的令牌当场清掉', () => {
    signedInAs('17')
    saveTokens([{ accountId: 58, email: 'me@163.com', token: 'tok-163' }])
    useMailbox(58)

    erpSessionExpires()
    keepTokensFor('25')

    expect(currentToken()).toBe('')
    expect(allTokens()).toEqual([])
    expect(unlockedMailboxes()).toEqual([])
  })

  it('换人清掉之后，再换回原来的人也不会凭空冒出令牌', () => {
    signedInAs('17')
    saveTokens([{ accountId: 58, email: 'me@163.com', token: 'tok-163' }])
    keepTokensFor('25')
    keepTokensFor('17')
    expect(allTokens()).toEqual([])
  })

  it('只有单数那一把的验证（旧后端、没绑箱的人）也记了名', () => {
    signedInAs('17')
    adoptVerification({ token: 'tok-pass' })
    erpSessionExpires()
    keepTokensFor('17')
    expect(currentToken()).toBe('tok-pass')
  })

  it('改版前没记名的令牌：登录那一刻分不清是谁的，清掉', () => {
    localStorage.setItem('mailUnlock', 'legacy')
    localStorage.setItem('mailUnlockTokens', '{"58":"legacy"}')
    keepTokensFor('17')
    expect(allTokens()).toEqual([])
  })

  it('改版那天正开着 ERP 的人：会话确认身份后认领，之后重新登录不清', () => {
    localStorage.setItem('mailUnlock', 'legacy')
    localStorage.setItem('mailUnlockTokens', '{"58":"legacy"}')
    claimUnstampedTokens('17')

    erpSessionExpires()
    keepTokensFor('17')

    expect(currentToken()).toBe('legacy')
    expect(unlockedMailboxes()).toEqual([58])
  })

  it('认领不改已经记了名的：验证时记下的比推断可靠', () => {
    signedInAs('17')
    saveTokens([{ accountId: 58, email: 'me@163.com', token: 'tok-163' }])
    claimUnstampedTokens('25')
    keepTokensFor('17')
    expect(allTokens()).toEqual(['tok-163'])
  })

  it('手上一把令牌都没有时，认领什么都不记', () => {
    claimUnstampedTokens('17')
    expect(localStorage.getItem('mailUnlockOwner')).toBeNull()
  })

  it('空的员工号不会把别人的令牌留下来', () => {
    signedInAs('17')
    saveTokens([{ accountId: 58, email: 'me@163.com', token: 'tok-163' }])
    keepTokensFor('')
    expect(allTokens()).toEqual([])
  })
})

// 守住 api.ts 那一处：ERP 登录过期时清掉的键里不能再有邮箱令牌。
// 那个函数不导出、又要走 axios 拦截器才能触发，所以对着源码核。
describe('api.ts 的 ERP 登录过期处理', () => {
  it('不清邮箱令牌', () => {
    const src = readFileSync(resolve(__dirname, '../api.ts'), 'utf8')
    const body = src.slice(src.indexOf('function expired()'))
    const list = body.slice(body.indexOf('for (const k of ['), body.indexOf(']) {'))
    expect(list).toContain("'employeeId'")
    expect(list).not.toMatch(/mailUnlock|UNLOCK/)
  })
})
