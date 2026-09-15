import { describe, expect, it } from 'vitest'
import { canBundleAttachments } from './attachmentBundle'

describe('给不给「下载全部」', () => {
  it('会话里每一封各算各的：三个的给，两个的也给', () => {
    expect(canBundleAttachments(3, '38249')).toBe(true)
    expect(canBundleAttachments(2, '38252')).toBe(true)
  })

  it('只有一个文件不给：和旁边那颗「下载」是同一件事', () => {
    expect(canBundleAttachments(1, '38249')).toBe(false)
    expect(canBundleAttachments(0, '38249')).toBe(false)
  })

  // 打包按信的编号取文件。会话里「我发出」而本地没留底的那几条没有编号——
  // 给了按钮点下去只会 404，那正是 2026-09-15 预览那条走过的老路。
  it('不知道是哪一封信不给，哪怕文件再多', () => {
    expect(canBundleAttachments(5, '')).toBe(false)
  })
})
