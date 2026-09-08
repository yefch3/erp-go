import { describe, expect, test } from 'vitest'
import { attachmentHintKey, INBOUND_ATTACHMENT_LIMIT } from './attachmentHint'

describe('附件那一行该说什么', () => {
  test('有下载地址就是正常的一行', () => {
    expect(attachmentHintKey({ downloadUrl: 'https://x/y', stored: true })).toBe('downloadFile')
  })

  // 留过底、只是这会儿签不出地址：刷新可能就好了。不能说成「没留底」——
  // 那是对别人的数据下一个我们并不知道的断言。
  test('留过底但暂时取不到地址，说的是「暂时」', () => {
    expect(attachmentHintKey({ stored: true })).toBe('fileUnavailable')
  })

  test('没留底又没有别的话可说，就说没留底', () => {
    expect(attachmentHintKey({ stored: false, fileSize: 1024 })).toBe('fileGone')
  })

  // 这条是这次修复的用户可见部分：超过上限的附件从此不再被截成一个打不开的
  // 文件，而是留一行说清楚为什么，并指向还能取回原件的那条路。
  test('太大而没留底的，要单独说，因为有路可走', () => {
    expect(attachmentHintKey({ stored: false, fileSize: 30 * 1024 * 1024 }))
      .toBe('fileTooLarge')
  })

  test('正好卡在上限上不算太大', () => {
    expect(attachmentHintKey({ stored: false, fileSize: INBOUND_ATTACHMENT_LIMIT }))
      .toBe('fileGone')
    expect(attachmentHintKey({ stored: false, fileSize: INBOUND_ATTACHMENT_LIMIT + 1 }))
      .toBe('fileTooLarge')
  })

  // protojson 把 int64 打成字符串，普通 JSON 打成数字。两条路都得走对——
  // 这个仓库为同一件事已经踩过一次（见 protoId.test.ts）。
  test('大小是字符串时也认得出来', () => {
    expect(attachmentHintKey({ stored: false, fileSize: String(30 * 1024 * 1024) }))
      .toBe('fileTooLarge')
  })

  // 太大的那些 stored 一定是 false（没留底），但万一哪天上游改了，
  // 「有地址」永远优先——有地址就是能下载，别再解释为什么。
  test('有地址时不管多大都是正常的一行', () => {
    expect(attachmentHintKey({ downloadUrl: 'https://x/y', fileSize: 99 * 1024 * 1024 }))
      .toBe('downloadFile')
  })
})
