import { describe, expect, it } from 'vitest'
import { canPreview, isSheetPreview, needsConversion } from './attachmentPreview'

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

  it('zip 这些：没有预览按钮', () => {
    for (const f of [{}, { previewKind: '' }, { previewKind: undefined }, { fileName: 'a.zip' }]) {
      expect(canPreview(f)).toBe(false)
      expect(needsConversion(f)).toBe(false)
    }
  })

  it('表格自己一档：有按钮，但永远不请服务器转', () => {
    for (const f of [
      { fileName: '装箱单.xlsx' },
      { fileName: 'list.csv' },
      { fileName: '无扩展名', contentType: 'text/csv' },
      // 后端照样标了 convert（它不知道前端会自己读），也不该去转。
      { fileName: '报价.xlsx', previewKind: 'convert' },
    ]) {
      expect(isSheetPreview(f)).toBe(true)
      expect(canPreview(f)).toBe(true)
      expect(needsConversion(f)).toBe(false)
    }
  })

  it('老的 .xls 读不了，还是走转 PDF 那条路', () => {
    const f = { fileName: 'legacy.xls', previewKind: 'convert' }
    expect(isSheetPreview(f)).toBe(false)
    expect(needsConversion(f)).toBe(true)
  })

  // 后端加了新的 kind 而前端还没跟上时，宁可不显示按钮，也不要显示一个
  // 点了没反应的按钮。
  it('不认识的 kind 当成不能预览', () => {
    const f = { previewKind: 'something-new' }
    expect(canPreview(f)).toBe(false)
    expect(needsConversion(f)).toBe(false)
  })
})
