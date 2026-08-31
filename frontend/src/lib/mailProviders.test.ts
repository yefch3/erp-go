import { describe, expect, it } from 'vitest'
import { readFileSync } from 'node:fs'
import { resolve } from 'node:path'
import { MAIL_PROVIDERS, PROVIDER_OTHER, providerByCode, providerForAddress } from './mailProviders'

describe('mailProviders', () => {
  it('按地址后缀认得出个人邮箱', () => {
    expect(providerForAddress('me@gmail.com')?.code).toBe('gmail')
    expect(providerForAddress('me@163.com')?.code).toBe('netease163')
    expect(providerForAddress('me@QQ.com')?.code).toBe('qq')
    expect(providerForAddress('me@foxmail.com')?.code).toBe('qq')
    expect(providerForAddress('me@hotmail.com')?.code).toBe('outlook')
  })

  it('认不出企业邮的域名，这是对的', () => {
    // sunrise.com 可能托管在 263、腾讯、阿里任何一家。猜错的代价是拿着
    // 凭据去登了另一家的服务器，而那个失败长得和「授权码错了」一样。
    expect(providerForAddress('me@sunrise.com')).toBeNull()
    expect(providerForAddress('nonsense')).toBeNull()
    expect(providerForAddress('me@')).toBeNull()
  })

  it('认不得的代号返回 null，不猜一个', () => {
    expect(providerByCode('does-not-exist')).toBeNull()
    expect(providerByCode('GMAIL')?.code).toBe('gmail')
  })

  it('每一家都有名字和「去哪儿拿授权码」', () => {
    // 提示语是这份清单最有用的部分：员工卡住的地方从来不是「填哪个服务器」，
    // 是「授权码在哪儿开」。
    for (const p of MAIL_PROVIDERS) {
      expect(p.label, p.code).not.toBe('')
      expect(p.hint.length, p.code).toBeGreaterThan(10)
    }
  })

  // 代号必须和服务端那张表一字不差：真正连哪台服务器由服务端按 code 查表
  // 决定，对不上时服务端报「认不出这个邮件服务商」，员工在界面上挑了一家
  // 却绑不上，而前端这边什么都不知道。
  //
  // 读 Go 源码做断言，和网关那几条钉子测试同一个路子：这件事没有一个运行
  // 时的地方能一眼看出来，而两边各改一半不会让任何别的测试变红。
  it('代号和服务端那张表对得上', () => {
    const go = readFileSync(
      resolve(__dirname, '../../../services/mail/internal/app/mailproviders.go'),
      'utf8',
    )
    const serverCodes = new Set<string>()
    for (const m of go.matchAll(/^\t\tCode:\s*"([a-z0-9]+)"/gm)) serverCodes.add(m[1])
    for (const m of go.matchAll(/^\t\tCode:\s+"([a-z0-9]+)"/gm)) serverCodes.add(m[1])
    expect(serverCodes.size, '没能从 mailproviders.go 里读出代号，正则该跟着改')
      .toBeGreaterThan(5)

    for (const p of MAIL_PROVIDERS) {
      expect(serverCodes.has(p.code), `服务端不认得 ${p.code}——员工挑了会绑不上`)
        .toBe(true)
    }
    // 反过来也查：服务端有而前端没有的，员工根本挑不到。
    for (const code of serverCodes) {
      expect(
        MAIL_PROVIDERS.some((p) => p.code === code),
        `服务端有 ${code} 而清单里没有——员工挑不到这一家`,
      ).toBe(true)
    }
    expect(go.includes(`ProviderCodeOther = "${PROVIDER_OTHER}"`)).toBe(true)
  })
})
