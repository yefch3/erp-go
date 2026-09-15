// 转换任务交付的结果，和它在页面上要变成的样子。
//
// 服务端从 2026-09-14 起不再随结果发行数据：workbook_json 只存 metadata
// （表名、说明、表头、字段标识），行只在 .xlsx 文件里——而文件本来就随
// 响应一起来了（下载按钮用的就是它）。所以预览的表格在这里从文件里解出
// 来，走的是客户发来的 Excel 附件同一条路（parseTableFile）。
//
// 文件里公式格旁边存着算好的值，解出来就是人要看的那个数，不是「=T3*U3」。

import { parseTableFile } from './attachmentExcel'

export interface ExcelSheet {
  name: string
  summary: string
  columns: string[]
  // 每列的模板字段标识；转采购按它对齐，没有它时退回表头文字。
  columnKeys?: string[]
  rows: { cells: string[] }[]
  totalRows: string
}

export interface ExcelResult {
  fileName: string
  fileData: string
  sheets: ExcelSheet[]
  model: string
  inquiryTemplateId?: string
  inquiryTemplateCode?: string
  inquiryTemplateVersion?: number
  // 文件解不出来时说给人听的那句话；表格空着，下载照常。
  previewError?: string
}

export function base64ToBytes(encoded: string): Uint8Array<ArrayBuffer> {
  const raw = atob(encoded)
  const bytes = new Uint8Array(raw.length)
  for (let i = 0; i < raw.length; i++) bytes[i] = raw.charCodeAt(i)
  return bytes
}

/**
 * 把行数据补进结果里。
 *
 * 每张表都已经带行的（改动前完成的旧任务）原样返回。否则从 fileData 解：
 * 文件里的表和 metadata 里的表按位置一一对应——写文件时就是按 sheets 的
 * 顺序写的。表头、说明、字段标识以 metadata 为准，行数和前 200 行取自文件。
 *
 * 解不出来就抛：文件是我们自己生成的，格式固定，解不出来是真出了事，
 * 调用方该让人知道，而不是给一张空表。
 */
export async function hydrateExcelResult(result: ExcelResult): Promise<ExcelResult> {
  if (result.sheets.length > 0 && result.sheets.every((sheet) => sheet.rows.length > 0)) return result
  const bytes = base64ToBytes(result.fileData)
  const parsed = await parseTableFile(result.fileName, bytes.buffer)
  if (parsed.sheets.length < result.sheets.length) {
    throw new Error(`workbook has ${parsed.sheets.length} sheets, metadata lists ${result.sheets.length}`)
  }
  return {
    ...result,
    sheets: result.sheets.map((sheet, index) => {
      const table = parsed.sheets[index]
      return {
        ...sheet,
        columns: sheet.columns.length > 0 ? sheet.columns : table.columns,
        rows: table.rows.map((cells) => ({ cells })),
        totalRows: String(table.totalRows),
      }
    }),
  }
}
