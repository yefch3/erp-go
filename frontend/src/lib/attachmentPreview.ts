/**
 * 附件能不能预览、点了之后去哪儿。
 *
 * 后端在附件上标了 previewKind：
 *   ""        只能下载
 *   "direct"  previewUrl 已经能用（图片、PDF）
 *   "office"  在线 Office 里打开（配了 OnlyOffice 时，Word/Excel/PPT 都是这档）
 *
 * 从前还有 "convert"：先请服务器转成 PDF 再看。2026-09-14 连同 Gotenberg
 * 一起退役，服务端不再发这个值——收到了也**不给按钮**，那条接口已经撤了。
 *
 * 前端自己还多认一档：**表格**（.xlsx/.csv/.tsv）。它们不走服务器——浏览器
 * 自己就读得动，在新标签页里直接画成表（见 pages/SheetWindowPage.vue）。
 * 服务端配了在线 Office 的话表格也归 office，所以这一档实际只剩 .tsv 和
 * 没有扩展名但内容类型是表格的那些。
 *
 * 抽出来是因为「显不显示预览按钮」和「点了之后做什么」是同一个判断的两半，
 * 分散在模板和函数里迟早会对不上——按钮出现了但点了没反应，是最难查的那种。
 */

import { isDirectTableFile } from './attachmentExcel'

export const PREVIEW_DIRECT = 'direct'
// 在线 Office 里打开：服务端配了 OnlyOffice 时，办公文档（连表格一起）都是
// 这一档。优先于浏览器自己画表格那一档——一个完整的在线 Excel 比自己画的强。
//
// 从前还有第三档 'convert'：先在服务器上转成 PDF 再看。2026-09-14 连同
// Gotenberg 一起退役——它认的扩展名是在线 Office 认的子集，而判断顺序是先问
// Office，所以在配了 Office 的部署里那一档根本走不到。
export const PREVIEW_OFFICE = 'office'

/** 在在线 Office 里开。 */
export function isOfficePreview(file: PreviewableFile): boolean {
  return file.previewKind === PREVIEW_OFFICE
}

export interface PreviewableFile {
  fileName?: string
  contentType?: string
  previewUrl?: string
  previewKind?: string
}

/** 浏览器自己读得动的表格：不转 PDF，在新标签页里画出来。 */
export function isSheetPreview(file: PreviewableFile): boolean {
  return isDirectTableFile(file.fileName ?? '', file.contentType ?? '')
}

/**
 * 这一档预览要不要知道「附件属于哪封信」。
 *
 * 在线 Office 和浏览器画表格都要：它们开的是 /mail/<信>/office|sheet/<附件>，
 * 服务端按那封信去找这个附件。图片和 PDF 不要——地址早签好了。
 */
export function previewNeedsOwningMail(file: PreviewableFile): boolean {
  return isOfficePreview(file) || isSheetPreview(file)
}

/**
 * 要不要显示「预览」按钮。
 *
 * mailID 是这个附件**自己所属的那封信**，没有就传空。要它是因为上面那两档
 * 离了它开不出来：会话里「我发出」的那几条，附件用的是另一套编号，拿当前
 * 打开的那封信去找必然找不到——2026-09-15 一个发出去的 .xlsx 点预览，得到
 * 的是「这个表格打不开，可能是文件损坏」，而文件好好的。
 *
 * 宁可不显示按钮，也不要显示一个点了必然报错的按钮。
 */
export function canPreview(file: PreviewableFile, mailID = ''): boolean {
  if (previewNeedsOwningMail(file)) return Boolean(mailID)
  return Boolean(file.previewUrl)
}
