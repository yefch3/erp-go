import { describe, expect, it } from 'vitest'
import cases from './passwordPolicy.cases.json'
import { checkPassword, passwordRules, type PasswordProblem } from './passwordPolicy'

// 共用样例：服务端那份（passwordpolicy_shared_test.go）读的是同一个文件。
// 谁改了规则忘了改另一边，两边有一边会红。
describe('密码规则（和服务端同一份样例）', () => {
  for (const c of cases.cases) {
    it(`${c.why}：${c.password.slice(0, 24) || '（空）'} → ${c.expect}`, () => {
      const got = checkPassword(c.password, c.context)
      expect(got ?? 'ok').toBe(c.expect)
    })
  }

  it('样例里每一种结果都出现过，别让某一条规则从此没人测', () => {
    const seen = new Set(cases.cases.map((c) => c.expect))
    for (const want of ['ok', 'tooShort', 'tooLong', 'tooCommon', 'tooSimple', 'fromIdentity']) {
      expect(seen.has(want)).toBe(true)
    }
  })

  it('样例里的问题名和 Go 的错误码一一对应', () => {
    const named = new Set(Object.keys(cases.problemToCode))
    for (const c of cases.cases) {
      if (c.expect !== 'ok') expect(named.has(c.expect)).toBe(true)
    }
  })
})

// 界面上那份清单要把四条**各算各的**：checkPassword 到第一条没过就停（它要和
// 服务端报同一句话），清单不行——人得看见还差哪几条。
describe('界面上那份清单', () => {
  const keys = (password: string, context: string[] = []) =>
    Object.fromEntries(passwordRules(password, context).map((r) => [r.key, r.ok]))

  it('一个好密码四条全过', () => {
    expect(keys('tRuck-plum-92')).toEqual({
      length: true, notCommon: true, notRepetitive: true, notIdentity: true,
    })
  })

  it('太短只标长度那一条，别的照样算', () => {
    expect(keys('ab')).toEqual({
      length: false, notCommon: true, notRepetitive: true, notIdentity: true,
    })
  })

  it('一个密码可以同时踩两条', () => {
    // 键盘顺序 + 六个连着走的字符。
    expect(keys('1234567890')).toEqual({
      length: true, notCommon: false, notRepetitive: false, notIdentity: true,
    })
  })

  it('撞上身份时说清撞的是哪个词', () => {
    const rule = passwordRules('zhangsan-8821', ['zhangsan']).find((r) => r.key === 'notIdentity')
    expect(rule?.ok).toBe(false)
    expect(rule?.detail).toBe('zhangsan')
  })

  it('空密码：长度没过，别的不冤枉它', () => {
    expect(keys('')).toEqual({
      length: false, notCommon: true, notRepetitive: true, notIdentity: true,
    })
  })
})

// 类型上钉一下：样例里的 expect 只能是这几种。
const _problems: Record<PasswordProblem | 'ok', true> = {
  ok: true, tooShort: true, tooLong: true, tooCommon: true, tooSimple: true, fromIdentity: true,
}
void _problems
