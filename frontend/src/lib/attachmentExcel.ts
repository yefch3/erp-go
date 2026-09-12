// Direct reader for attachments that already ARE spreadsheets. An .xlsx or
// .csv from a customer needs no model call to be shown: the bytes come from
// the same signed URL the download button uses, and this parser turns them
// into the preview shape the Excel dialog renders. Anything this file cannot
// read (.xls, odd encodings, exotic sheet features) falls back to the model
// path, and 重新生成 always pays for a fresh model read.

export interface DirectSheet {
  name: string
  columns: string[]
  rows: string[][]
  // Rows beyond the preview cap are counted here, mirroring the model path's
  // totalRows so the dialog's "first N of M" note keeps working.
  totalRows: number
}

export interface DirectWorkbook {
  sheets: DirectSheet[]
}

// 默认的上限，给「导入用的预览」：那条路上人要看的是"表头对不对、前几行长
// 什么样"，不是整张表。
const maxSheets = 20
const maxColumns = 80
const maxPreviewRows = 200

/** 读多少。不传就是导入预览那套默认值。 */
export interface TableLimits {
  maxSheets?: number
  maxColumns?: number
  maxRows?: number
}

function limitsOf(l?: TableLimits) {
  return {
    sheets: l?.maxSheets ?? maxSheets,
    columns: l?.maxColumns ?? maxColumns,
    rows: l?.maxRows ?? maxPreviewRows,
  }
}

export function isDirectTableFile(name: string, contentType = ''): boolean {
  const dot = name.lastIndexOf('.')
  const ext = dot >= 0 ? name.slice(dot).toLowerCase() : ''
  if (ext === '.xlsx' || ext === '.csv' || ext === '.tsv') return true
  const ct = contentType.toLowerCase().split(';')[0].trim()
  return (
    ct === 'application/vnd.openxmlformats-officedocument.spreadsheetml.sheet' ||
    ct === 'text/csv' ||
    ct === 'text/tab-separated-values'
  )
}

export async function parseTableFile(
  name: string,
  data: ArrayBuffer,
  limits?: TableLimits,
): Promise<DirectWorkbook> {
  const dot = name.lastIndexOf('.')
  const ext = dot >= 0 ? name.slice(dot).toLowerCase() : ''
  if (ext === '.csv' || ext === '.tsv') {
    const text = new TextDecoder('utf-8').decode(data)
    return delimitedWorkbook(sheetNameFromFile(name), text, ext === '.tsv' ? '\t' : ',', limits)
  }
  return xlsxWorkbook(new Uint8Array(data), limits)
}

function sheetNameFromFile(name: string): string {
  const base = name.split(/[\\/]/).pop() ?? name
  const dot = base.lastIndexOf('.')
  return (dot > 0 ? base.slice(0, dot) : base).trim() || 'Sheet1'
}

// ---------------------------------------------------------------- delimited

function delimitedWorkbook(
  sheetName: string,
  text: string,
  delimiter: string,
  limits?: TableLimits,
): DirectWorkbook {
  const rows = parseDelimited(text.replace(/^﻿/, ''), delimiter).filter((row) => row.some((cell) => cell.trim() !== ''))
  if (rows.length === 0) throw new Error('empty table file')
  return { sheets: [sheetFromRows(sheetName, rows, limits)] }
}

function parseDelimited(text: string, delimiter: string): string[][] {
  const rows: string[][] = []
  let row: string[] = []
  let field = ''
  let inQuotes = false
  let i = 0
  while (i < text.length) {
    const ch = text[i]
    if (inQuotes) {
      if (ch === '"') {
        if (text[i + 1] === '"') {
          field += '"'
          i += 2
          continue
        }
        inQuotes = false
        i++
        continue
      }
      field += ch
      i++
      continue
    }
    if (ch === '"' && field === '') {
      inQuotes = true
      i++
      continue
    }
    if (ch === delimiter) {
      row.push(field)
      field = ''
      i++
      continue
    }
    if (ch === '\n' || ch === '\r') {
      if (ch === '\r' && text[i + 1] === '\n') i++
      row.push(field)
      field = ''
      rows.push(row)
      row = []
      i++
      continue
    }
    field += ch
    i++
  }
  if (field !== '' || row.length > 0) {
    row.push(field)
    rows.push(row)
  }
  return rows
}

function sheetFromRows(name: string, rows: string[][], limits?: TableLimits): DirectSheet {
  const cap = limitsOf(limits)
  const columns = rows[0].slice(0, cap.columns)
  const data = rows.slice(1)
  return {
    name,
    columns,
    rows: data.slice(0, cap.rows).map((row) => {
      const cells = row.slice(0, columns.length)
      while (cells.length < columns.length) cells.push('')
      return cells
    }),
    totalRows: data.length,
  }
}

// ---------------------------------------------------------------- xlsx
//
// The small OOXML subset a data workbook needs: shared strings, inline
// strings, plain numbers, booleans. No DOMParser — the tests run under Node,
// which has none — so the XML is read with scanners that understand exactly
// these parts and nothing more.

interface ZipMember {
  method: number
  offset: number
  compressedSize: number
}

// Uint8Array<ArrayBuffer> 而不是光写 Uint8Array：TypeScript 5.7 起给这个
// 类型加了「背后是哪种 buffer」的参数，而 Blob 只收普通 ArrayBuffer 撑着
// 的那一种。这里的字节一路来自 File.arrayBuffer()，本来就是普通的；把它
// 写出来，比在下面某一行加断言诚实。
async function xlsxWorkbook(bytes: Uint8Array<ArrayBuffer>, limits?: TableLimits): Promise<DirectWorkbook> {
  const cap = limitsOf(limits)
  const members = zipMembers(bytes)
  const workbookXml = await memberText(bytes, members, 'xl/workbook.xml')
  if (workbookXml === undefined) throw new Error('not an xlsx: no workbook part')

  const rels = new Map<string, string>()
  const relsXml = (await memberText(bytes, members, 'xl/_rels/workbook.xml.rels')) ?? ''
  for (const match of relsXml.matchAll(/<Relationship\b[^>]*>/g)) {
    const id = xmlAttr(match[0], 'Id')
    const target = xmlAttr(match[0], 'Target')
    if (id && target) rels.set(id, target.startsWith('/') ? target.slice(1) : `xl/${target}`)
  }

  const shared: string[] = []
  const sharedXml = await memberText(bytes, members, 'xl/sharedStrings.xml')
  if (sharedXml !== undefined) {
    for (const si of sharedXml.matchAll(/<si>([\s\S]*?)<\/si>/g)) {
      shared.push(collectText(si[1]))
    }
  }

  // 哪些单元格样式是日期。没有它，一份装箱单里的「交期」是 45789——一串
  // 谁都读不懂的数字，而这正是 Excel 里最常见的那一列。见 dateStyles。
  const dateStyle = await dateStyleSet(bytes, members)

  const sheets: DirectSheet[] = []
  for (const match of workbookXml.matchAll(/<sheet\b[^>]*>/g)) {
    if (sheets.length >= cap.sheets) break
    const name = xmlAttr(match[0], 'name') || `Sheet${sheets.length + 1}`
    const rid = xmlAttr(match[0], 'r:id')
    const part = rid ? rels.get(rid) : undefined
    if (!part) continue
    const xml = await memberText(bytes, members, part)
    if (xml === undefined) continue
    const rows = worksheetRows(xml, shared, dateStyle, cap.rows)
    if (rows.length > 0) sheets.push(sheetFromRows(name, rows, limits))
  }
  if (sheets.length === 0) throw new Error('not an xlsx: no readable sheet')
  return { sheets }
}

// keep 是"最多留几行的内容"。超过之后照样一行一行数下去（"共 8 万行"这句话
// 要准），但不再拆里面的格子——一张 8 万行的表，光是把每个单元格解出来再扔掉
// 就够把这一页卡死，而卡死之前人连"只画了前 2000 行"那句话都看不到。
function worksheetRows(xml: string, shared: string[], dateStyle?: Set<number>, keep = Infinity): string[][] {
  const rows: string[][] = []
  let seen = 0
  for (const rowMatch of xml.matchAll(/<row\b[^>]*>([\s\S]*?)<\/row>/g)) {
    // 表头那一行也占一个名额，所以是 keep + 1。
    if (++seen > keep + 1) {
      rows.push([])
      continue
    }
    const cells: string[] = []
    for (const cellMatch of rowMatch[1].matchAll(/<c\b([^>]*?)(?:\/>|>([\s\S]*?)<\/c>)/g)) {
      const attrs = cellMatch[1]
      const body = cellMatch[2] ?? ''
      const ref = xmlAttr(attrs, 'r')
      const index = ref ? columnIndex(ref) : cells.length
      const type = xmlAttr(attrs, 't')
      let value = ''
      if (type === 's') {
        value = shared[Number(firstTagText(body, 'v'))] ?? ''
      } else if (type === 'inlineStr') {
        value = collectText(body)
      } else {
        value = firstTagText(body, 'v')
        // 数字格式说这是日期，就按日期写出来。
        const style = Number(xmlAttr(attrs, 's'))
        if (value !== '' && dateStyle?.has(Number.isFinite(style) ? style : -1)) {
          value = excelSerialToText(Number(value)) || value
        }
      }
      while (cells.length < index) cells.push('')
      cells[index] = value
    }
    rows.push(cells)
  }
  return rows
}

// ------------------------------------------------------------- 日期
//
// Excel 里的日期就是一个数：1899-12-30 起的天数。哪些单元格该当日期读，写在
// styles.xml 里——单元格上的 s="3" 是 cellXfs 的第 3 条，那条的 numFmtId 指向
// 一个数字格式。内置的 14–17、22、45–47 是日期和时间；自定义格式（numFmtId
// ≥ 164）要看它的 formatCode 里有没有 y/m/d/h/s。
//
// 不做这件事的样子是：一份装箱单的「交期」那一列整列显示 45789。这是收到的
// 表格里最常见的一列，也是最容易让人以为「这个预览是坏的」的一列。
const builtinDateFormats = new Set([14, 15, 16, 17, 18, 19, 20, 21, 22, 45, 46, 47])

async function dateStyleSet(
  bytes: Uint8Array<ArrayBuffer>,
  members: Map<string, ZipMember>,
): Promise<Set<number>> {
  const out = new Set<number>()
  let xml: string | undefined
  try {
    xml = await memberText(bytes, members, 'xl/styles.xml')
  } catch {
    // 样式读不了不该让整张表读不出来：大不了日期还是那串数字。
    return out
  }
  if (xml === undefined) return out

  // 自定义格式：formatCode 里出现 y/m/d/h/s（引号里的字面量除外）才算日期。
  const dateFmtIds = new Set<number>()
  for (const m of xml.matchAll(/<numFmt\b[^>]*>/g)) {
    const id = Number(xmlAttr(m[0], 'numFmtId'))
    const code = xmlAttr(m[0], 'formatCode')
    if (!Number.isFinite(id) || !code) continue
    if (/[ymdhs]/i.test(code.replace(/"[^"]*"/g, '').replace(/\\./g, ''))) dateFmtIds.add(id)
  }

  // cellXfs 的顺序就是单元格 s 属性的编号。
  const cellXfs = xml.match(/<cellXfs\b[^>]*>([\s\S]*?)<\/cellXfs>/)
  if (!cellXfs) return out
  let index = 0
  for (const xf of cellXfs[1].matchAll(/<xf\b[^>]*>/g)) {
    const id = Number(xmlAttr(xf[0], 'numFmtId'))
    if (Number.isFinite(id) && (builtinDateFormats.has(id) || dateFmtIds.has(id))) out.add(index)
    index++
  }
  return out
}

/**
 * 天数变成「2026-09-12」。带小数的再加上时分。
 *
 * 起点是 1899-12-30，不是 12-31：Excel 认为 1900 年有 2 月 29 日（它没有），
 * 所以 1900-03-01 之后的每个数都比真实天数大一天，把起点往前挪一天正好抵消。
 * 1900 年 1、2 月的日期因此会差一天——那是 Excel 自己的历史包袱，各家表格
 * 软件都这么将错就错，而 1900 年的日期不会出现在装箱单上。
 */
export function excelSerialToText(serial: number): string {
  if (!Number.isFinite(serial) || serial <= 0) return ''
  const ms = Math.round(serial * 86400000)
  const at = new Date(Date.UTC(1899, 11, 30) + ms)
  if (Number.isNaN(at.getTime())) return ''
  const pad = (n: number) => String(n).padStart(2, '0')
  const day = `${at.getUTCFullYear()}-${pad(at.getUTCMonth() + 1)}-${pad(at.getUTCDate())}`
  // 只有日期的那些数是整数（或者差一点点，浮点的锅）。
  const dayFraction = serial - Math.floor(serial)
  if (dayFraction < 1 / 86400) return day
  const time = `${pad(at.getUTCHours())}:${pad(at.getUTCMinutes())}`
  return `${day} ${time}`
}

function columnIndex(ref: string): number {
  let n = 0
  for (const ch of ref) {
    if (ch < 'A' || ch > 'Z') break
    n = n * 26 + (ch.charCodeAt(0) - 64)
  }
  return Math.max(0, n - 1)
}

function firstTagText(xml: string, tag: string): string {
  const match = xml.match(new RegExp(`<${tag}(?:\\s[^>]*)?>([\\s\\S]*?)</${tag}>`))
  return match ? xmlUnescape(match[1]) : ''
}

// An <si> or <is> holds either one <t> or a run of rich-text <r><t> pieces;
// the text is the concatenation either way.
function collectText(xml: string): string {
  let out = ''
  for (const t of xml.matchAll(/<t(?:\s[^>]*)?>([\s\S]*?)<\/t>/g)) {
    out += t[1]
  }
  return xmlUnescape(out)
}

function xmlAttr(tag: string, name: string): string {
  const match = tag.match(new RegExp(`(?:^|\\s)${name.replace(':', '\\:')}="([^"]*)"`))
  return match ? xmlUnescape(match[1]) : ''
}

function xmlUnescape(value: string): string {
  return value.replace(/&(#x?[0-9a-fA-F]+|amp|lt|gt|quot|apos);/g, (all, entity: string) => {
    switch (entity) {
      case 'amp': return '&'
      case 'lt': return '<'
      case 'gt': return '>'
      case 'quot': return '"'
      case 'apos': return "'"
      default: {
        const code = entity.startsWith('#x') ? parseInt(entity.slice(2), 16) : parseInt(entity.slice(1), 10)
        return Number.isFinite(code) ? String.fromCodePoint(code) : all
      }
    }
  })
}

// ------------------------------------------------------------- zip reading

function zipMembers(bytes: Uint8Array<ArrayBuffer>): Map<string, ZipMember> {
  const view = new DataView(bytes.buffer, bytes.byteOffset, bytes.byteLength)
  // End of central directory: within the last 64 KiB plus its own 22 bytes.
  let eocd = -1
  for (let i = bytes.length - 22; i >= Math.max(0, bytes.length - 22 - 0xffff); i--) {
    if (view.getUint32(i, true) === 0x06054b50) {
      eocd = i
      break
    }
  }
  if (eocd < 0) throw new Error('not a zip file')
  const count = view.getUint16(eocd + 10, true)
  let offset = view.getUint32(eocd + 16, true)
  const members = new Map<string, ZipMember>()
  const decoder = new TextDecoder()
  for (let i = 0; i < count; i++) {
    if (view.getUint32(offset, true) !== 0x02014b50) throw new Error('corrupt zip directory')
    const method = view.getUint16(offset + 10, true)
    const compressedSize = view.getUint32(offset + 20, true)
    const nameLength = view.getUint16(offset + 28, true)
    const extraLength = view.getUint16(offset + 30, true)
    const commentLength = view.getUint16(offset + 32, true)
    const localOffset = view.getUint32(offset + 42, true)
    const name = decoder.decode(bytes.subarray(offset + 46, offset + 46 + nameLength))
    members.set(name, { method, offset: localOffset, compressedSize })
    offset += 46 + nameLength + extraLength + commentLength
  }
  return members
}

async function memberText(bytes: Uint8Array<ArrayBuffer>, members: Map<string, ZipMember>, name: string): Promise<string | undefined> {
  const member = members.get(name)
  if (!member) return undefined
  const view = new DataView(bytes.buffer, bytes.byteOffset, bytes.byteLength)
  if (view.getUint32(member.offset, true) !== 0x04034b50) throw new Error('corrupt zip entry')
  const nameLength = view.getUint16(member.offset + 26, true)
  const extraLength = view.getUint16(member.offset + 28, true)
  const start = member.offset + 30 + nameLength + extraLength
  const slice = bytes.subarray(start, start + member.compressedSize)
  let raw: Uint8Array<ArrayBuffer>
  if (member.method === 0) {
    raw = slice
  } else if (member.method === 8) {
    const stream = new Blob([slice]).stream().pipeThrough(new DecompressionStream('deflate-raw'))
    raw = new Uint8Array(await new Response(stream).arrayBuffer())
  } else {
    throw new Error(`unsupported zip method ${member.method}`)
  }
  return new TextDecoder('utf-8').decode(raw)
}
