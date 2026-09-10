import { describe, expect, test } from 'vitest'
import { turnRecipients, turnSenderEmail, turnSenderLabel } from './threadTurn'

// 被报上来的那个问题：同一条会话里，上下两行的地址一个是「发给谁」、一个是
// 「谁发的」，而两行长得一模一样。这一组钉住的就是「两行的同一个位置说的是
// 同一件事」。
describe('两个方向说同一件事', () => {
  const received = {
    direction: 'IN',
    counterparty: 'ana@buyer.example',
    fromEmail: 'ana@buyer.example',
    fromName: 'Ana Costa',
    toAll: 'me@263.net, colleague@263.net',
  }
  const sent = {
    direction: 'OUT',
    counterparty: 'ana@buyer.example',
    fromEmail: 'me@263.net',
    fromName: '我',
    toAll: 'ana@buyer.example',
  }

  test('发件人永远是写信的那个人，不是对方', () => {
    expect(turnSenderEmail(received)).toBe('ana@buyer.example')
    expect(turnSenderEmail(sent)).toBe('me@263.net')
  })

  test('收件人永远是收信的那一方', () => {
    expect(turnRecipients(received)).toBe('me@263.net, colleague@263.net')
    expect(turnRecipients(sent)).toBe('ana@buyer.example')
  })

  // 这一条是问题本身：两行的 counterparty 一样（都是 ana@buyer.example），
  // 照着它显示就分不出谁发谁收。发件人必须不一样。
  test('counterparty 相同时，发件人仍然分得开', () => {
    expect(received.counterparty).toBe(sent.counterparty)
    expect(turnSenderEmail(received)).not.toBe(turnSenderEmail(sent))
  })

  test('有名字显示名字，没有就显示地址', () => {
    expect(turnSenderLabel(received)).toBe('Ana Costa')
    expect(turnSenderLabel({ ...received, fromName: '' })).toBe('ana@buyer.example')
  })
})

// 前端会先于后端发上去，那一会儿 fromEmail / toAll 是空的。
describe('后端还没带上新字段时', () => {
  test('收到的那行仍然认得出发件人', () => {
    const it = { direction: 'IN', counterparty: 'ana@buyer.example' }
    expect(turnSenderEmail(it)).toBe('ana@buyer.example')
    // 收件人这一腿老后端给不出，宁可空着。
    expect(turnRecipients(it)).toBe('')
  })

  test('发出的那行仍然认得出收件人', () => {
    const it = { direction: 'OUT', counterparty: 'ana@buyer.example' }
    expect(turnRecipients(it)).toBe('ana@buyer.example')
  })

  // 关键：老后端在「我发出」那行给的 counterparty 是收件人。把它当成发件人
  // 显示，正是原来那个 bug。宁可空着。
  test('绝不把收件人说成发件人', () => {
    const it = { direction: 'OUT', counterparty: 'ana@buyer.example' }
    expect(turnSenderEmail(it)).toBe('')
  })
})
