import { describe, expect, it } from 'vitest'
import { attachmentExcelSource } from './attachmentExcelSource'

describe('attachmentExcelSource', () => {
  it('用的是附件自己所属的那封信，不是打开的那封', () => {
    // 打开的是最早那封（21059），点的是回信（36816）里的附件——发给服务器的
    // 必须是回信的号。2026-09-15 那次「附件不存在或未保存」就是这里错了。
    expect(attachmentExcelSource('36816', '21246')).toEqual({
      kind: 'attachment',
      mailId: '36816',
      attachmentId: '21246',
    })
  })

  it('发出去的信的附件不能转：没有信号就不给菜单', () => {
    expect(attachmentExcelSource('', '63')).toBeNull()
  })
})
