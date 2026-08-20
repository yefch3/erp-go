import { describe, expect, it } from 'vitest'

import { isDialogDismissed } from './dialogActions'

describe('isDialogDismissed', () => {
  it('把取消和关闭识别为正常退出', () => {
    expect(isDialogDismissed('cancel')).toBe(true)
    expect(isDialogDismissed('close')).toBe(true)
  })

  it('不会吞掉真正的异常', () => {
    expect(isDialogDismissed(new Error('network failed'))).toBe(false)
    expect(isDialogDismissed('confirm')).toBe(false)
  })
})
