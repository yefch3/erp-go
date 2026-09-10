import { describe, expect, it } from 'vitest'
import { replyAllRecipients } from './replyAll'

const seven = ['a', 'b', 'c', 'd', 'e', 'f', 'g'].map((x) => ({ name: x.toUpperCase(), email: `${x}@co.com` }))

describe('回复全部', () => {
  // 用户撞上的那个场景：客户群发给公司七个人，我是 A，点回复全部。
  it('收件人是发信人，其余六个同事进抄送，我自己不在里面', () => {
    const r = replyAllRecipients({
      fromEmail: 'client@buyer.com',
      fromName: 'Marilin',
      toParties: seven,
      ccParties: [{ email: 'boss@co.com' }],
      self: 'A@co.com',
    })
    expect(r.to).toEqual({ name: 'Marilin', email: 'client@buyer.com' })
    expect(r.cc.map((p) => p.email)).toEqual([
      'b@co.com', 'c@co.com', 'd@co.com', 'e@co.com', 'f@co.com', 'g@co.com', 'boss@co.com',
    ])
  })

  it('有 Reply-To 就回到那儿；它本身在 To 里时不再抄一次', () => {
    const r = replyAllRecipients({
      fromEmail: 'noreply@buyer.com',
      replyTo: 'sales@buyer.com',
      toParties: [{ email: 'me@co.com' }, { name: 'Sales', email: 'sales@buyer.com' }],
      self: 'me@co.com',
    })
    expect(r.to).toEqual({ name: 'Sales', email: 'sales@buyer.com' })
    expect(r.cc).toEqual([])
  })

  // 规则变过：原来去掉的是名下**全部**信箱，现在只去掉这次回信用的那一个。
  // 换的原因写在 replyAll.ts 顶上——几个箱常常是不同业务线，客户同时发给
  // 两个箱是有意的，把另一个箱丢掉，那条线上的同事就看不到这轮往来了。
  it('名下另一个箱照常进抄送，只有正在回信的这个箱被去掉', () => {
    const r = replyAllRecipients({
      fromEmail: 'client@buyer.com',
      toParties: [{ email: 'me@qq.com' }, { email: 'me@163.com' }, { email: 'peer@co.com' }],
      self: 'me@qq.com',
    })
    expect(r.cc.map((p) => p.email)).toEqual(['me@163.com', 'peer@co.com'])
  })

  it('大小写不同的同一个地址算一个', () => {
    const r = replyAllRecipients({
      fromEmail: 'Client@Buyer.com',
      toParties: [{ email: 'Peer@co.com' }, { email: 'peer@CO.com' }],
      self: '',
    })
    expect(r.to.email).toBe('client@buyer.com')
    expect(r.cc.map((p) => p.email)).toEqual(['peer@co.com'])
  })

  it('没拆出任何人（老信还没补收件人）：等于普通回复', () => {
    const r = replyAllRecipients({ fromEmail: 'client@buyer.com', self: 'me@co.com' })
    expect(r.to.email).toBe('client@buyer.com')
    expect(r.cc).toEqual([])
  })
})

// 一个人绑了两个信箱，一封信正好发给这两个箱。
//
// 这是线上真发生过的：抄送要去掉本人名下**全部**信箱的地址，于是算出来是空的。
// 从前按钮的存在绑在这个结果上，人看到的就是「明明发给了多个人，却没有回复
// 全部」。规则本身是对的（不该抄送自己），错的是拿它决定按钮在不在——按钮
// 现在常驻，抄送为空时退化成一次普通回复。
describe('收件人里有我自己名下的箱', () => {
  it('只有正在回信的那个箱被去掉，剩下的照常抄', () => {
    const r = replyAllRecipients({
      fromEmail: 'client@buyer.com',
      fromName: 'Ana',
      toParties: [
        { name: '', email: 'me@co.com' },
        { name: 'Me', email: 'me2@gmail.com' },
      ],
      self: 'me@co.com',
    })
    expect(r.to).toEqual({ name: 'Ana', email: 'client@buyer.com' })
    expect(r.cc).toEqual([{ name: 'Me', email: 'me2@gmail.com' }])
  })

  it('只有一个箱是我的时候，另一个人照常进抄送', () => {
    const r = replyAllRecipients({
      fromEmail: 'client@buyer.com',
      toParties: [
        { name: '', email: 'me@co.com' },
        { name: '同事', email: 'colleague@co.com' },
      ],
      self: 'me@co.com',
    })
    expect(r.cc).toEqual([{ name: '同事', email: 'colleague@co.com' }])
  })
})
