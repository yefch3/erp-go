import { describe, expect, it } from 'vitest'
import { syncBanner } from './syncBanner'

describe('同步错误横幅', () => {
  it('授权码被拒：显示文本，给「重新登录」按钮', () => {
    const b = syncBanner({ detail: '邮箱拒绝了这个授权码：LOGIN Login error', needsReauth: true })
    expect(b.text).toContain('授权码')
    expect(b.offerReauth).toBe(true)
  })

  // 生产上真发生的：263 掐线 → 横幅 → 员工重输授权码 → 没用 → 几分钟后再来。
  it('服务器掐线 / 超时：显示文本，但不劝人重登', () => {
    for (const detail of [
      '收取邮件失败：imap: connection closed',
      '打开 INBOX 失败：EXAMINE Unsafe Login. Please contact kefu@188.com',
      '连接 imap.263.net:993 失败：i/o timeout',
    ]) {
      const b = syncBanner({ detail, needsReauth: false })
      expect(b.text).toBe(detail)
      expect(b.offerReauth).toBe(false)
    }
  })

  it('后端没给 needsReauth（老版本）当作不劝重登：宁可少一颗按钮', () => {
    expect(syncBanner({ detail: 'x' }).offerReauth).toBe(false)
  })

  it('没有错误就没有横幅', () => {
    expect(syncBanner({})).toEqual({ text: '', offerReauth: false })
    expect(syncBanner({ detail: '   ' }).text).toBe('')
  })

  it('信箱清单里的 lastError 走同一条规则', () => {
    expect(syncBanner({ lastError: '授权码被撤销', needsReauth: true }).offerReauth).toBe(true)
    expect(syncBanner({ lastError: 'imap: connection closed', needsReauth: false }).offerReauth).toBe(false)
  })
})
