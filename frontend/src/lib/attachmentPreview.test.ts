import { describe, expect, it } from 'vitest'
import { canPreview, needsConversion } from './attachmentPreview'

describe('附件预览的两个判断', () => {
  it('图片和 PDF：直接就能看，不用转', () => {
    const f = { previewUrl: 'https://files/x.png', previewKind: 'direct' }
    expect(canPreview(f)).toBe(true)
    expect(needsConversion(f)).toBe(false)
  })

  it('Word / Excel：按钮要出现，点下去要先转', () => {
    const f = { previewKind: 'convert' }
    expect(canPreview(f)).toBe(true)
    expect(needsConversion(f)).toBe(true)
  })

  it('转过一次之后就不再转', () => {
    const f = { previewKind: 'convert', previewUrl: 'https://files/x.pdf' }
    expect(canPreview(f)).toBe(true)
    expect(needsConversion(f)).toBe(false)
  })

  it('zip、csv 这些：没有预览按钮', () => {
    for (const f of [{}, { previewKind: '' }, { previewKind: undefined }]) {
      expect(canPreview(f)).toBe(false)
      expect(needsConversion(f)).toBe(false)
    }
  })

  // 后端加了新的 kind 而前端还没跟上时，宁可不显示按钮，也不要显示一个
  // 点了没反应的按钮。
  it('不认识的 kind 当成不能预览', () => {
    const f = { previewKind: 'something-new' }
    expect(canPreview(f)).toBe(false)
    expect(needsConversion(f)).toBe(false)
  })
})
