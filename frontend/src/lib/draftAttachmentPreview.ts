// 写信窗口里还没发出去的附件，点一下能看一眼。
//
// 收到的附件有一行记录，服务端顺手就把下载和预览地址签好放在列表里了（见
// MailAttachments）。草稿的附件没有那一行——浏览器把文件直传进对象存储，只拿
// 回一个 key，登记要等到发送那一刻。所以地址得点的时候现要。
//
// 三档，和收信那边同一套词：
//   direct  图片和 PDF：服务端签好的内联地址，新标签页直接显示
//   sheet   .xlsx / .csv / .tsv：浏览器自己解出来画成表（和收信那边同一个页面）
//   ''      其余：只给下载
//
// 没有 office 那一档。在线 Office 的取件地址是按「哪封信的哪个附件」签出来
// 的，草稿的附件两样都还没有，所以 Word 和 PPT 在这里只能下载——按钮不出现，
// 好过点了打不开。

import { isDirectTableFile } from './attachmentExcel'

/** 服务端签回来的一份地址。 */
export interface DraftAttachmentLinks {
  fileKey: string
  fileName: string
  contentType?: string
  fileSize?: string
  downloadUrl?: string
  previewUrl?: string
  previewKind?: string
}

export type DraftPreviewKind = 'direct' | 'sheet' | ''

/**
 * 这份附件点了之后去哪儿。
 *
 * 表格排在 direct 后面：服务端不会把表格标成 direct（内联只认图片和 PDF），
 * 所以两者不会打架；顺序写出来是为了让规则一眼看得全。
 */
export function draftPreviewKind(file: DraftAttachmentLinks): DraftPreviewKind {
  if (file.previewKind === 'direct' && file.previewUrl) return 'direct'
  if (file.downloadUrl && isDirectTableFile(file.fileName ?? '', file.contentType ?? '')) {
    return 'sheet'
  }
  return ''
}

/** 要不要显示「预览」按钮。 */
export function canPreviewDraft(file: DraftAttachmentLinks): boolean {
  return draftPreviewKind(file) !== ''
}

/**
 * 表格预览页的地址。
 *
 * 用 key 而不是「哪封信的哪个附件」：草稿还没有信。页面拿着 key 回头向
 * /email-attachments/preview 要一次地址，和这里要的是同一份东西。
 */
export function draftSheetHref(file: DraftAttachmentLinks): string {
  const q = new URLSearchParams({ key: file.fileKey, name: file.fileName ?? '' })
  return `/attachment/sheet?${q.toString()}`
}
