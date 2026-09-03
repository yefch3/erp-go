/**
 * 附件能不能预览、要不要先转一趟。
 *
 * 后端在附件上标了 previewKind：
 *   ""        只能下载
 *   "direct"  previewUrl 已经能用（图片、PDF）
 *   "convert" 是办公文档，要先请服务器转成 PDF 才有地址
 *
 * 抽出来是因为「显不显示预览按钮」和「点了之后做什么」是同一个判断的两半，
 * 分散在模板和函数里迟早会对不上——按钮出现了但点了没反应，是最难查的那种。
 */

export const PREVIEW_DIRECT = 'direct'
export const PREVIEW_CONVERT = 'convert'

export interface PreviewableFile {
  previewUrl?: string
  previewKind?: string
}

/** 要不要显示「预览」按钮。 */
export function canPreview(file: PreviewableFile): boolean {
  return Boolean(file.previewUrl) || file.previewKind === PREVIEW_CONVERT
}

/**
 * 点下去要不要先请服务器转一趟。
 *
 * 转过一次之后 previewUrl 就填上了，再点就直接开——所以这里同时看两个字段，
 * 不然同一份报价单每次点开都要再问一次服务器。
 */
export function needsConversion(file: PreviewableFile): boolean {
  return file.previewKind === PREVIEW_CONVERT && !file.previewUrl
}
