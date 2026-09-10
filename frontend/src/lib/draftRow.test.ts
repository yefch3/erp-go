import { describe, expect, test } from 'vitest'
import { draftToRow, draftWho } from './draftRow'

const labels = {
  noRecipient: '(无收件人)',
  andMore: (first: string, n: number) => `${first} 等 ${n} 人`,
}

describe('draftWho', () => {
  test('一个收件人：有名字用名字', () => {
    expect(draftWho({ id: '1', recipients: [{ email: 'lin@b.com', name: '林采购' }] }, labels)).toBe(
      '林采购',
    )
  })

  test('没名字就用地址——名字是认得出的那个，地址是一定有的那个', () => {
    expect(draftWho({ id: '1', recipients: [{ email: 'lin@b.com' }] }, labels)).toBe('lin@b.com')
  })

  test('多个人说「等 N 人」，不把地址全排下去', () => {
    const d = {
      id: '1',
      recipients: [{ email: 'a@b.com', name: '甲' }, { email: 'c@d.com' }, { email: 'e@f.com' }],
    }
    expect(draftWho(d, labels)).toBe('甲 等 3 人')
  })

  test('一个收件人都没有时说出来，不留一行空白', () => {
    expect(draftWho({ id: '1' }, labels)).toBe('(无收件人)')
    expect(draftWho({ id: '1', recipients: [] }, labels)).toBe('(无收件人)')
  })

  test('名字和地址都空的条目不算数', () => {
    expect(draftWho({ id: '1', recipients: [{ email: '', name: '' }] }, labels)).toBe('(无收件人)')
  })

  test('服务端数的人数更多时听服务端的——说多了比说少了错得轻', () => {
    const d = { id: '1', recipients: [{ email: 'a@b.com', name: '甲' }], recipientCount: 4 }
    expect(draftWho(d, labels)).toBe('甲 等 4 人')
  })
})

describe('draftToRow', () => {
  const d = {
    id: '7',
    subject: '报价确认',
    snippet: '附上最新报价',
    updatedAt: '2026-03-01T08:00:00Z',
    recipients: [{ email: 'lin@b.com', name: '林采购' }],
    hasAttachments: true,
  }

  test('保存时间就是列表那一列的时间', () => {
    expect(draftToRow(d, labels).receivedAt).toBe('2026-03-01T08:00:00Z')
  })

  test('写给谁放在「已发送」那条路上，不是伪装成发件人', () => {
    const row = draftToRow(d, labels)
    expect(row.toAll).toBe('林采购')
    expect(row.fromName).toBe('')
    expect(row.fromEmail).toBe('')
  })

  test('永远是已读：草稿是自己写的，加粗在这份列表里的意思是没看过', () => {
    expect(draftToRow(d, labels).isRead).toBe(true)
  })

  test('永远没有星标：星标要写到邮件服务器上，草稿没有正本可标', () => {
    expect(draftToRow(d, labels).isStarred).toBe(false)
  })

  test('主题、摘要、时间三样全空也照样是一行能点的东西', () => {
    const row = draftToRow({ id: '9' }, labels)
    expect(row.id).toBe('9')
    expect(row.subject).toBe('')
    expect(row.snippet).toBe('')
    expect(row.receivedAt).toBe('')
    expect(row.toAll).toBe('(无收件人)')
  })

  test('带附件的标出来', () => {
    expect(draftToRow(d, labels).hasAttachments).toBe(true)
    expect(draftToRow({ id: '9' }, labels).hasAttachments).toBe(false)
  })
})
