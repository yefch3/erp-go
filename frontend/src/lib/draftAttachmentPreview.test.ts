import { describe, expect, it } from 'vitest'

import { canPreviewDraft, draftPreviewKind, draftSheetHref } from './draftAttachmentPreview'

// 「按钮出现了但点了没反应」是最难查的那种毛病，所以「显不显示按钮」和
// 「点了去哪儿」用的是同一个判断，这里一起钉住。

describe('draftPreviewKind', () => {
  it('PDF 和图片：服务端签好了内联地址，直接显示', () => {
    expect(draftPreviewKind({
      fileKey: 'mail-attachments/4/a.pdf', fileName: '报价单.pdf',
      previewKind: 'direct', previewUrl: 'https://files/x?inline',
    })).toBe('direct')
  })

  it('标了 direct 却没有地址：不给按钮，不是给一个点了空白的按钮', () => {
    expect(draftPreviewKind({
      fileKey: 'k', fileName: '报价单.pdf', previewKind: 'direct',
    })).toBe('')
  })

  it('表格：浏览器自己画，靠的是下载地址', () => {
    for (const name of ['报价单.xlsx', '清单.csv', 'data.tsv']) {
      expect(draftPreviewKind({
        fileKey: 'k', fileName: name, downloadUrl: 'https://files/x',
      })).toBe('sheet')
    }
  })

  it('表格但连下载地址都没签出来：不给按钮', () => {
    expect(draftPreviewKind({ fileKey: 'k', fileName: '报价单.xlsx' })).toBe('')
  })

  it('Word 和 PPT 在这条路上只能下载', () => {
    for (const name of ['合同.docx', '介绍.pptx', '存档.zip']) {
      expect(draftPreviewKind({
        fileKey: 'k', fileName: name, downloadUrl: 'https://files/x',
      })).toBe('')
    }
  })
})

describe('canPreviewDraft', () => {
  it('和 draftPreviewKind 是同一个判断的两半', () => {
    const cases = [
      { fileKey: 'k', fileName: 'a.pdf', previewKind: 'direct', previewUrl: 'u' },
      { fileKey: 'k', fileName: 'a.xlsx', downloadUrl: 'u' },
      { fileKey: 'k', fileName: 'a.docx', downloadUrl: 'u' },
      { fileKey: 'k', fileName: 'a.pdf' },
    ]
    for (const c of cases) {
      expect(canPreviewDraft(c)).toBe(draftPreviewKind(c) !== '')
    }
  })
})

describe('draftSheetHref', () => {
  it('带上 key 和文件名，中文和空格都要能原样传过去', () => {
    const href = draftSheetHref({
      fileKey: 'mail-attachments/4/9f2c.xlsx', fileName: '报价单 (2).xlsx',
    })
    const q = new URLSearchParams(href.slice(href.indexOf('?') + 1))
    expect(href.startsWith('/attachment/sheet?')).toBe(true)
    expect(q.get('key')).toBe('mail-attachments/4/9f2c.xlsx')
    expect(q.get('name')).toBe('报价单 (2).xlsx')
  })
})
