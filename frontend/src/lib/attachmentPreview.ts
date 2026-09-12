/**
 * 附件能不能预览、要不要先转一趟。
 *
 * 后端在附件上标了 previewKind：
 *   ""        只能下载
 *   "direct"  previewUrl 已经能用（图片、PDF）
 *   "convert" 是办公文档，要先请服务器转成 PDF 才有地址
 *
 * 前端自己还多认一档：**表格**（.xlsx/.csv/.tsv）。它们不走服务器——浏览器
 * 自己就读得动，在新标签页里直接画成表（见 pages/SheetWindowPage.vue）。
 * 所以 .csv 现在也有预览按钮了，尽管后端从来没给过它 previewKind：后端那
 * 一档说的是「要不要转 PDF」，而表格不转。
 *
 * 抽出来是因为「显不显示预览按钮」和「点了之后做什么」是同一个判断的两半，
 * 分散在模板和函数里迟早会对不上——按钮出现了但点了没反应，是最难查的那种。
 */

import { isDirectTableFile } from './attachmentExcel'

export const PREVIEW_DIRECT = 'direct'
export const PREVIEW_CONVERT = 'convert'

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

/** 要不要显示「预览」按钮。 */
export function canPreview(file: PreviewableFile): boolean {
  return isSheetPreview(file) || Boolean(file.previewUrl) || file.previewKind === PREVIEW_CONVERT
}

/**
 * 点下去要不要先请服务器转一趟。
 *
 * 表格永远不用：那条路整个绕开了服务器。
 *
 * 其余的转过一次之后 previewUrl 就填上了，再点就直接开——所以这里同时看两个
 * 字段，不然同一份合同每次点开都要再问一次服务器。
 */
export function needsConversion(file: PreviewableFile): boolean {
  if (isSheetPreview(file)) return false
  return file.previewKind === PREVIEW_CONVERT && !file.previewUrl
}
