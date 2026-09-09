// 从剪贴板粘进来的表格，还原成一个真的表格。
//
// Issue #363：从别处复制一块表格粘到正文里，表格「被缩得非常小」。查出来
// 的原因和描述的不一样——**它根本没被当成表格**：
//
//   1. onPaste 先看剪贴板里有没有图片。从 Excel 复制一块区域时，剪贴板里
//      **同时有图片**，于是走了图片那条路。
//   2. 插入图片那行把宽度写死成 160px。所以「缩得非常小」是字面意义上的，
//      不是缩放算法的问题，是一个硬编码的缩略图宽度。
//
// 这个模块解决第 1 步：认出「这是一块表格」，用剪贴板里的**纯文本**重建
// 一个干净的表格。
//
// **为什么用纯文本而不是 text/html。** Excel、Sheets、Word、网页表格全都
// 会往剪贴板里放一份制表符分隔的纯文本，这是几十年的惯例，可靠。而它们那份
// text/html 是另一回事：满是类名、<style> 块、mso- 私有属性和邮件客户端
// 渲染不了的布局。要用它就得写一个 HTML 净化器，而：
//
//   · 这个仓库的前端测试跑在纯 node 里，没有 DOMParser——净化器写出来
//     测不了，而净化器恰恰是最不能靠"看着对"的那种代码
//   · 从别人的剪贴板来的 HTML 是不可信输入，直接塞进 contenteditable 是
//     一个真实的注入口子
//
// 用纯文本重建，等于**一个字节的外来 HTML 都不进文档**：进来的只有文字，
// 标签全是我们自己写的。代价是丢掉合并单元格和单元格格式（见下面）。

/** 重建出来的表格用的边框样式。写成行内样式，因为邮件客户端会丢掉 <style>。 */
const CELL = 'border:1px solid #d0d0d0;padding:4px 8px;'
const TABLE = 'border-collapse:collapse;width:100%;'

/**
 * Excel 有时给每行末尾多一个制表符，于是多出一个全空的列。去掉它——但
 * **要整块一起判断，不能逐行判断**。
 *
 * 逐行判断会把一个本来就空的最后一格当成多余的制表符删掉，那一行就比别人
 * 少一列，接着整块被矩形检查判定成"不是表格"。粘一个最后一列有空格的表格
 * 从此变成粘一段纯文字，而且不会有任何提示。（第一版就是这么写的，测试
 * 「中间的空行」当场红。）
 *
 * 整块判断不会有这个问题：要么所有行一起少一列，要么都不动，矩形永远成立。
 * 条件里要求原本至少三列，否则「两列的表格，第二列整列是空的」会被削成一列。
 */
function dropTrailingEmptyColumn(rows: string[][]): string[][] {
  const width = rows[0]?.length ?? 0
  if (width < 3) return rows
  if (!rows.every((r) => r.length === width && r[width - 1] === '')) return rows
  return rows.map((r) => r.slice(0, -1))
}

const ESCAPES: Record<string, string> = {
  '&': '&amp;',
  '<': '&lt;',
  '>': '&gt;',
  '"': '&quot;',
  "'": '&#39;',
}

function esc(s: string): string {
  return s.replace(/[&<>"']/g, (c) => ESCAPES[c])
}

/**
 * 剪贴板里这份东西是不是一块表格；是就回一段可以直接插入的 HTML，不是回 null。
 *
 * 两个条件都要满足，缺一个就当普通文字处理：
 *
 *   · html 里出现过 `<table`。这是「对方确实认为自己在复制一个表格」的信号。
 *     **只做字符串包含判断，这份 HTML 一个字节都不会进文档**——它只用来
 *     回答"是不是表格"这一个问题。
 *   · 纯文本能解析成一个**矩形**：至少两行、至少两列、每行列数一致。
 *
 * 第二条是防误判的关键。一段带制表符的代码、一份对齐过的清单都可能有制表符，
 * 但它们的每行列数不齐；把它们变成表格比不变更糟。
 */
export function tableFromClipboard(html: string, text: string): string | null {
  if (!html.toLowerCase().includes('<table')) return null

  // \r\n 和 \r 都归一成 \n：Excel 在 Windows 上给的是 \r\n。
  const lines = text.replace(/\r\n?/g, '\n').split('\n')
  // 末尾的空行去掉，Excel 结尾常多给一个换行。中间的空行留着——它在表格里
  // 是一个空行，不是分隔符。
  while (lines.length && lines[lines.length - 1] === '') lines.pop()
  if (lines.length < 2) return null

  // 不 trim 每一格：单元格里本来的前后空格是内容。
  const rows = dropTrailingEmptyColumn(lines.map((l) => l.split('\t')))
  const width = rows[0].length
  if (width < 2) return null
  if (rows.some((r) => r.length !== width)) return null

  // 第一行当表头。表格类应用复制出来的第一行几乎总是标题，而 <th> 在所有
  // 邮件客户端里都是加粗居中的，不需要额外样式。猜错的代价是第一行被加粗，
  // 比整个表格没有表头小。
  const head = `<tr>${rows[0].map((c) => `<th style="${CELL}">${esc(c)}</th>`).join('')}</tr>`
  const body = rows
    .slice(1)
    .map((r) => `<tr>${r.map((c) => `<td style="${CELL}">${esc(c)}</td>`).join('')}</tr>`)
    .join('')
  return `<table style="${TABLE}"><thead>${head}</thead><tbody>${body}</tbody></table>`
}

/**
 * 插入图片时用多宽。
 *
 * 从前写死 160px（issue #363 里「表格很小」的直接原因）。现在按图片自己的
 * 宽度来，只在超过正文宽度时才收——收的时候按比例，不然图会被压扁。
 *
 * 上限是 600：邮件正文在几乎所有客户端里都是 600 出头，超过就要横向滚动，
 * 而收件人多半是在手机上看。
 *
 * 量不到尺寸（图还没加载完）时回 0，让调用方干脆不写 width 属性——不写比
 * 写一个猜的数好，浏览器会用图片的真实尺寸，`max-width:100%` 兜住上限。
 */
export const MAIL_BODY_WIDTH = 600

export function imageWidth(naturalWidth: number): number {
  if (!Number.isFinite(naturalWidth) || naturalWidth <= 0) return 0
  return Math.min(Math.round(naturalWidth), MAIL_BODY_WIDTH)
}
