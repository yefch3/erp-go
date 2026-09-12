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

const maxSheets = 20
const maxColumns = 80
const maxPreviewRows = 200

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

export async function parseTableFile(name: string, data: ArrayBuffer, rowLimit = maxPreviewRows): Promise<DirectWorkbook> {
  const dot = name.lastIndexOf('.')
  const ext = dot >= 0 ? name.slice(dot).toLowerCase() : ''
  if (ext === '.csv' || ext === '.tsv') {
    const text = new TextDecoder('utf-8').decode(data)
    return delimitedWorkbook(sheetNameFromFile(name), text, ext === '.tsv' ? '\t' : ',', rowLimit)
  }
  return xlsxWorkbook(new Uint8Array(data), rowLimit)
}

function sheetNameFromFile(name: string): string {
  const base = name.split(/[\\/]/).pop() ?? name
  const dot = base.lastIndexOf('.')
  return (dot > 0 ? base.slice(0, dot) : base).trim() || 'Sheet1'
}

// ---------------------------------------------------------------- delimited

function delimitedWorkbook(sheetName: string, text: string, delimiter: string, rowLimit: number): DirectWorkbook {
  const rows = parseDelimited(text.replace(/^﻿/, ''), delimiter).filter((row) => row.some((cell) => cell.trim() !== ''))
  if (rows.length === 0) throw new Error('empty table file')
  return { sheets: [sheetFromRows(sheetName, rows, rowLimit)] }
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

function sheetFromRows(name: string, rows: string[][], rowLimit: number): DirectSheet {
  const columns = rows[0].slice(0, maxColumns)
  const data = rows.slice(1)
  return {
    name,
    columns,
    rows: data.slice(0, rowLimit).map((row) => {
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
async function xlsxWorkbook(bytes: Uint8Array<ArrayBuffer>, rowLimit: number): Promise<DirectWorkbook> {
  const members = zipMembers(bytes)
  const workbookXml = await memberText(bytes, members, 'xl/workbook.xml')
  if (workbookXml === undefined) throw new Error('not an xlsx: no workbook part')

  const rels = new Map<string, string>()
  const relsXml = (await memberText(bytes, members, 'xl/_rels/workbook.xml.rels')) ?? ''
  for (const match of relsXml.matchAll(/<(?:[A-Za-z_][\w.-]*:)?Relationship\b[^>]*>/g)) {
    const id = xmlAttr(match[0], 'Id')
    const target = xmlAttr(match[0], 'Target')
    if (id && target) rels.set(id, target.startsWith('/') ? target.slice(1) : `xl/${target}`)
  }

  const shared: string[] = []
  const sharedXml = await memberText(bytes, members, 'xl/sharedStrings.xml')
  if (sharedXml !== undefined) {
    for (const si of sharedXml.matchAll(/<(?:[A-Za-z_][\w.-]*:)?si\b[^>]*>([\s\S]*?)<\/(?:[A-Za-z_][\w.-]*:)?si>/g)) {
      shared.push(collectText(si[1]))
    }
  }

  const sheets: DirectSheet[] = []
  for (const match of workbookXml.matchAll(/<(?:[A-Za-z_][\w.-]*:)?sheet\b[^>]*>/g)) {
    if (sheets.length >= maxSheets) break
    const name = xmlAttr(match[0], 'name') || `Sheet${sheets.length + 1}`
    const rid = xmlAttr(match[0], 'r:id')
    const part = rid ? rels.get(rid) : undefined
    if (!part) continue
    const xml = await memberText(bytes, members, part)
    if (xml === undefined) continue
    const rows = worksheetRows(xml, shared)
    if (rows.length > 0) sheets.push(sheetFromRows(name, rows, rowLimit))
  }
  if (sheets.length === 0) throw new Error('not an xlsx: no readable sheet')
  return { sheets }
}

function worksheetRows(xml: string, shared: string[]): string[][] {
  const rows: string[][] = []
  for (const rowMatch of xml.matchAll(/<(?:[A-Za-z_][\w.-]*:)?row\b[^>]*>([\s\S]*?)<\/(?:[A-Za-z_][\w.-]*:)?row>/g)) {
    const cells: string[] = []
    for (const cellMatch of rowMatch[1].matchAll(/<(?:[A-Za-z_][\w.-]*:)?c\b([^>]*?)(?:\/>|>([\s\S]*?)<\/(?:[A-Za-z_][\w.-]*:)?c>)/g)) {
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
      }
      while (cells.length < index) cells.push('')
      cells[index] = value
    }
    rows.push(cells)
  }
  return rows
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
  const qualified = `(?:[A-Za-z_][\\w.-]*:)?${tag}`
  const match = xml.match(new RegExp(`<${qualified}(?:\\s[^>]*)?>([\\s\\S]*?)</${qualified}>`))
  return match ? xmlUnescape(match[1]) : ''
}

// An <si> or <is> holds either one <t> or a run of rich-text <r><t> pieces;
// the text is the concatenation either way.
function collectText(xml: string): string {
  let out = ''
  for (const t of xml.matchAll(/<(?:[A-Za-z_][\w.-]*:)?t(?:\s[^>]*)?>([\s\S]*?)<\/(?:[A-Za-z_][\w.-]*:)?t>/g)) {
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
