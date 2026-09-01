import { beforeEach, describe, expect, it } from 'vitest'
import { readFileSync } from 'node:fs'
import { resolve } from 'node:path'
import { adoptVerification, currentToken, initialMailbox, useMailbox } from './mailUnlock'

// vitest 在 node 环境跑，没有 localStorage。八行够用，不为此装 jsdom。
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

// 一次验证回来的样子，照着网关 writeUnlockJSON 那张 map 写。
function response(boxes: { id: number; email: string }[], verified: number) {
  const tokens = boxes.map((b) => ({
    accountId: b.id,
    email: b.email,
    token: `tok-${b.id}`,
  }))
  const hit = tokens.find((t) => t.accountId === verified)
  return {
    token: hit?.token ?? tokens[0]?.token ?? '',
    tokens,
    expiresIn: 43200,
    detail: '',
    accountId: verified,
    email: hit?.email ?? '',
  }
}

describe('adoptVerification', () => {
  beforeEach(() => {
    ;(globalThis as unknown as { localStorage: MemoryStorage }).localStorage =
      new MemoryStorage()
  })

  // 这一条钉的是一个真实发生过的 bug：绑好第二个信箱之后点它，弹回邮箱
  // 登录页。信箱确实绑上了，服务端也确实把两把令牌都发下来了——是前端
  // 把 tokens 那个数组丢了，于是 mailUnlockTokens 一直是空表。
  //
  // 症状里最会骗人的一点：**第一个箱是好的**。单数那个 token 字段还在写
  // mailUnlock，所以自己的箱看着一切正常，只有切换才坏。
  it('绑好第二个信箱之后，切过去不该再问一次密码', () => {
    adoptVerification(response([{ id: 4, email: 'a@263.net' }], 4))
    expect(currentToken()).toBe('tok-4')

    // 再加一个。服务端这一次回的是两个箱各一把。
    adoptVerification(
      response(
        [
          { id: 4, email: 'a@263.net' },
          { id: 7, email: 'erptest@263.net' },
        ],
        7,
      ),
    )
    // 刚验的那个箱成为当前。
    expect(currentToken()).toBe('tok-7')
    // **两个都切得过去。** 任何一个返回 false，界面上就是「点一下弹回登录页」。
    expect(useMailbox(4)).toBe(true)
    expect(currentToken()).toBe('tok-4')
    expect(useMailbox(7)).toBe(true)
  })

  it('服务端没发 tokens（旧后端）时退回单数那一把，不把人锁在外面', () => {
    adoptVerification({ token: 'legacy-tok' })
    expect(currentToken()).toBe('legacy-tok')
  })

  it('accountId 是字符串也认——JSON 里的 int64 有两种写法', () => {
    adoptVerification({
      token: 'tok-9',
      tokens: [{ accountId: 9, email: 'x@y.com', token: 'tok-9' }],
      accountId: '9',
    })
    expect(useMailbox(9)).toBe(true)
  })

  it('一个箱都没绑的人拿到一把通行证，也要存下来', () => {
    // tokens 是空数组、accountId 是 0：活动和草稿那几个页面要进得去。
    adoptVerification({ token: 'pass', tokens: [], accountId: 0 })
    expect(currentToken()).toBe('pass')
  })
})

// 这个 bug 的本质是「前后端对同一个响应的字段名理解不一致」，而那种错
// TypeScript 一个字都不会说——响应体是 `as` 断言出来的，断言错了就是错了。
// 所以拿网关的源码来对。
describe('和网关的响应对得上', () => {
  const go = readFileSync(
    resolve(__dirname, '../../../services/gateway/internal/httpapi/mailunlock.go'),
    'utf8',
  )

  it('writeUnlockJSON 里确实有 token / tokens / accountId 这几个键', () => {
    const body = go.slice(go.indexOf('writeUnlockJSON(w, map[string]any{'))
    for (const key of ['"token"', '"tokens"', '"accountId"', '"email"']) {
      expect(body.slice(0, 600), `响应里少了 ${key}`).toContain(key)
    }
  })

  it('每个信箱一把令牌，字段名是 accountId / email / token', () => {
    // minted 那个结构体就是 tokens 数组里每一项的样子。
    const minted = go.slice(go.indexOf('type minted struct'))
    expect(minted.slice(0, 400)).toContain('`json:"accountId"`')
    expect(minted.slice(0, 400)).toContain('`json:"token"`')
  })

  it('服务端是按信箱逐个发的，不是一个人一把', () => {
    // 这一条钉的是口径本身：改回「一个人一把」的话，tokens 里就只会有一项，
    // 而切换信箱要重新输密码——那正是这一整批改动要消掉的事。
    expect(go).toContain('for _, b := range boxes.GetAccounts()')
    expect(go).toContain('s.Unlock.Grant(r.Context(), op.TenantID, op.EmployeeID, b.GetId())')
  })
})

// 打开邮箱页时左侧高亮哪个箱。
//
// 这一条钉的是「令牌那个箱」必须参与决定。漏掉它的表现是：从菜单点进 邮箱，
// 高亮落在默认箱 A，而请求带的是上次留下的 B 的令牌——网关只认令牌，于是
// 左边高亮 A、右边列的是 B 的信。而且**不会自己纠正**：切换那个 watch 要求
// 前一个值非空，0 → A 这一跳被它跳过了。
describe('initialMailbox', () => {
  it('地址栏说了算——后退/前进/分享的链接', () => {
    expect(initialMailbox({ url: 4, token: 7, fallback: 9 })).toBe(4)
  })

  it('地址栏没说时，用**令牌那个箱**，不是默认箱', () => {
    // 漏掉 token 这一路的话这里会回 9（默认箱），而信是从 7 那个箱拉的。
    expect(initialMailbox({ token: 7, fallback: 9 })).toBe(7)
  })

  it('两个都没有才落默认箱', () => {
    expect(initialMailbox({ fallback: 9 })).toBe(9)
  })

  it('全没有回 0，让调用方自己兜底', () => {
    expect(initialMailbox({})).toBe(0)
    // 旧令牌和「一个箱都没绑」的人，accountId 就是 0。
    expect(initialMailbox({ url: 0, token: 0, fallback: 0 })).toBe(0)
  })
})

describe('lock-status 也要带上是哪个箱', () => {
  const go = readFileSync(
    resolve(__dirname, '../../../services/gateway/internal/httpapi/mailunlock.go'),
    'utf8',
  )

  it('mailLockStatus 回的是 unlocked + accountId', () => {
    const body = go.slice(go.indexOf('func (s *Server) mailLockStatus'))
    expect(body.slice(0, 800)).toContain('"unlocked"')
    // 少了这个键，页面就只能靠默认箱猜自己站在哪儿。
    expect(body.slice(0, 800)).toContain('"accountId"')
  })
})
