import { describe, expect, it } from 'vitest'
import { mailDetailRows, replyToDiffers } from './mailDetails'

// 翻译函数在这里只要能认出是哪一行，所以原样返回键名。
const t = (k: string) => k
const keys = (rows: { k: string }[]) => rows.map((r) => r.k)

describe('回信地址和发件人不一致', () => {
  it('没有回信地址就没有这回事', () => {
    expect(replyToDiffers({ fromEmail: 'a@x.com' })).toBe(false)
  })

  it('大小写和空格不算不一致', () => {
    expect(replyToDiffers({ fromEmail: 'a@x.com', replyTo: ' A@X.com ' })).toBe(false)
  })

  it('换了个地址就是不一致——这正是骗走货款最常用的一手', () => {
    expect(replyToDiffers({ fromEmail: 'a@x.com', replyTo: 'a@x-supplier.com' })).toBe(true)
  })
})

describe('详情里那几行', () => {
  it('没有信就没有行，不是一串空格', () => {
    expect(mailDetailRows(null, t)).toEqual([])
  })

  it('空字段整行不出现', () => {
    const rows = mailDetailRows({ fromEmail: 'a@x.com', toEmail: 'me@y.com' }, t)
    expect(keys(rows)).toEqual(['emails.detail.from', 'emails.detail.to'])
  })

  it('收件人给整段，不是第一个——客户群发给七个人时要看到七个', () => {
    const rows = mailDetailRows(
      { fromEmail: 'a@x.com', toEmail: 'me@y.com', toAll: 'me@y.com, you@y.com' },
      t,
    )
    expect(rows.find((r) => r.k === 'emails.detail.to')?.v).toBe('me@y.com, you@y.com')
  })

  it('回信地址只在和发件人不同时才列', () => {
    const same = mailDetailRows({ fromEmail: 'a@x.com', replyTo: 'a@x.com' }, t)
    expect(keys(same)).not.toContain('emails.detail.replyTo')
    const other = mailDetailRows({ fromEmail: 'a@x.com', replyTo: 'b@x.com' }, t)
    expect(keys(other)).toContain('emails.detail.replyTo')
  })

  it('发件人带名字时写成「名字 <地址>」', () => {
    const rows = mailDetailRows({ fromEmail: 'a@x.com', fromName: '老王' }, t)
    expect(rows[0].v).toBe('老王 <a@x.com>')
  })

  it('大小是人话，不是字节数', () => {
    const rows = mailDetailRows({ fromEmail: 'a@x.com', rawSize: 2048 }, t)
    expect(rows.find((r) => r.k === 'emails.detail.size')?.v).toBe('2 KB')
  })
})
