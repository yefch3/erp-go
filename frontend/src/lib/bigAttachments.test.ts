import { describe, expect, test } from 'vitest'
import { linkedAttachmentFlags, MAX_CARRIED_ATTACHMENT_BYTES } from './bigAttachments'

const mb = (n: number) => ({ size: n * 1024 * 1024 })

// 这一组例子和服务端 biglinks_test.go 钉的是同一批——两份实现必须给出同样的
// 答案，否则写信框标的和实际发出去的对不上，而那种错没人会当场发现。
describe('哪几个附件会变成链接', () => {
  test('装得下就全带，一个都不标', () => {
    expect(linkedAttachmentFlags([mb(2), mb(3)])).toEqual([false, false])
  })

  test('装不下就从大的开始转，直到剩下的装得下', () => {
    // 小 4 + 大 12 + 中 6 = 22 MB，转掉 12 之后剩 10，装得下。
    expect(linkedAttachmentFlags([mb(4), mb(12), mb(6)])).toEqual([false, true, false])
  })

  test('要转两个的时候也只转到刚好装得下为止', () => {
    // 9 + 1 + 9 + 6 = 25；转 9 剩 16 仍超；再转另一个 9 剩 7，够了。
    expect(linkedAttachmentFlags([mb(9), mb(1), mb(9), mb(6)]))
      .toEqual([true, false, true, false])
  })

  test('一个文件自己就超线，不该把别的也拖下水', () => {
    expect(linkedAttachmentFlags([mb(60), mb(1)])).toEqual([true, false])
  })

  test('同样大的按原顺序，不能每次标的不一样', () => {
    for (let i = 0; i < 5; i++) {
      expect(linkedAttachmentFlags([mb(10), mb(10)])).toEqual([true, false])
    }
  })

  test('正好卡在线上算装得下', () => {
    expect(linkedAttachmentFlags([{ size: MAX_CARRIED_ATTACHMENT_BYTES }])).toEqual([false])
    expect(linkedAttachmentFlags([{ size: MAX_CARRIED_ATTACHMENT_BYTES + 1 }])).toEqual([true])
  })

  test('没有附件时不报错', () => {
    expect(linkedAttachmentFlags([])).toEqual([])
  })
})
