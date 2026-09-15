import { describe, expect, it } from 'vitest'
import { canPreview, isOfficePreview, isSheetPreview } from './attachmentPreview'

describe('附件能不能预览、在哪儿预览', () => {
  it('图片和 PDF：地址已经填好了，直接开', () => {
    const f = { previewUrl: 'https://files/x.png', previewKind: 'direct' }
    expect(canPreview(f)).toBe(true)
    expect(isOfficePreview(f)).toBe(false)
  })

  it('zip 这些：没有预览按钮', () => {
    for (const f of [{}, { previewKind: '' }, { previewKind: undefined }, { fileName: 'a.zip' }]) {
      expect(canPreview(f)).toBe(false)
    }
  })

  it('表格那一档：浏览器自己读得动，不经过服务器', () => {
    for (const f of [
      { fileName: '装箱单.xlsx' },
      { fileName: 'list.csv' },
      { fileName: '无扩展名', contentType: 'text/csv' },
    ]) {
      expect(isSheetPreview(f)).toBe(true)
      expect(canPreview(f)).toBe(true)
    }
  })

  it('在线 Office 那一档：办公文档连表格一起归它', () => {
    for (const f of [
      { fileName: '合同.docx', previewKind: 'office' },
      { fileName: '装箱单.xlsx', previewKind: 'office' },
      { fileName: 'old.xls', previewKind: 'office' },
    ]) {
      expect(isOfficePreview(f)).toBe(true)
      expect(canPreview(f)).toBe(true)
    }
  })

  // 「先转成 PDF 再看」那一档（previewKind: 'convert'）2026-09-14 连同
  // Gotenberg 一起退役了。服务端不再发这个值；万一从哪儿冒出来一个，**不能
  // 给按钮**——那颗按钮背后已经没有接口了，点了只会得到一句错。
  //
  // 这条不是形式主义：换版本的那几分钟里，旧后端发出来的列表还带着 'convert'。
  it('退役了的 convert：认不出来，所以不给按钮', () => {
    const f = { fileName: '合同.docx', previewKind: 'convert' }
    expect(isOfficePreview(f)).toBe(false)
    expect(canPreview(f)).toBe(false)
  })

  // 后端加了新的 kind 而前端还没跟上时，宁可不显示按钮，也不要显示一个
  // 点了没反应的按钮。
  it('不认识的 kind 当成不能预览', () => {
    expect(canPreview({ previewKind: 'something-new' })).toBe(false)
  })
})
