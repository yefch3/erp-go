import { deflateRawSync, inflateRawSync } from 'node:zlib'
import { describe, expect, it } from 'vitest'
import { isDirectTableFile, parseTableFile } from './attachmentExcel'

// Every current browser accepts 'deflate-raw' here; the stripped Node build
// running these tests does not. Back it with zlib so the deflate path is
// still exercised end to end.
try {
  void new DecompressionStream('deflate-raw')
} catch {
  const Native = globalThis.DecompressionStream
  globalThis.DecompressionStream = class {
    constructor(format: string) {
      if (format !== 'deflate-raw') return new Native(format as 'deflate') as TransformStream<Uint8Array, Uint8Array>
      const chunks: Uint8Array[] = []
      return new TransformStream({
        transform(chunk, controller) {
          chunks.push(chunk as Uint8Array)
          void controller
        },
        flush(controller) {
          const all = new Uint8Array(chunks.reduce((sum, chunk) => sum + chunk.length, 0))
          let at = 0
          for (const chunk of chunks) {
            all.set(chunk, at)
            at += chunk.length
          }
          controller.enqueue(inflateRawSync(all))
        },
      }) as TransformStream<Uint8Array, Uint8Array>
    }
  } as typeof DecompressionStream
}

// ------------------------------------------------------------ zip builder
// Builds the small stored/deflated zip subset the parser reads, so tests can
// fabricate real xlsx containers without a fixture file or a zip dependency.

const crcTable = new Int32Array(256).map((_, n) => {
  let c = n
  for (let k = 0; k < 8; k++) c = c & 1 ? 0xedb88320 ^ (c >>> 1) : c >>> 1
  return c
})

function crc32(data: Uint8Array): number {
  let crc = -1
  for (const byte of data) crc = crcTable[(crc ^ byte) & 0xff] ^ (crc >>> 8)
  return (crc ^ -1) >>> 0
}

interface ZipInput {
  name: string
  body: string
  deflate?: boolean
}

// 返回类型写上 <ArrayBuffer>：不写的话 .buffer 是 ArrayBufferLike，切出来
// 的东西带着 SharedArrayBuffer 的可能性，而被测函数收的是普通 ArrayBuffer。
function buildZip(entries: ZipInput[]): Uint8Array<ArrayBuffer> {
  const encoder = new TextEncoder()
  const chunks: Uint8Array[] = []
  const central: Uint8Array[] = []
  let offset = 0
  for (const entry of entries) {
    const name = encoder.encode(entry.name)
    const plain = encoder.encode(entry.body)
    const data = entry.deflate ? new Uint8Array(deflateRawSync(plain)) : plain
    const method = entry.deflate ? 8 : 0
    const crc = crc32(plain)

    const local = new Uint8Array(30 + name.length)
    const lv = new DataView(local.buffer)
    lv.setUint32(0, 0x04034b50, true)
    lv.setUint16(4, 20, true)
    lv.setUint16(8, method, true)
    lv.setUint32(14, crc, true)
    lv.setUint32(18, data.length, true)
    lv.setUint32(22, plain.length, true)
    lv.setUint16(26, name.length, true)
    local.set(name, 30)
    chunks.push(local, data)

    const center = new Uint8Array(46 + name.length)
    const cv = new DataView(center.buffer)
    cv.setUint32(0, 0x02014b50, true)
    cv.setUint16(4, 20, true)
    cv.setUint16(6, 20, true)
    cv.setUint16(10, method, true)
    cv.setUint32(16, crc, true)
    cv.setUint32(20, data.length, true)
    cv.setUint32(24, plain.length, true)
    cv.setUint16(28, name.length, true)
    cv.setUint32(42, offset, true)
    center.set(name, 46)
    central.push(center)

    offset += local.length + data.length
  }
  const directory = concat(central)
  const end = new Uint8Array(22)
  const ev = new DataView(end.buffer)
  ev.setUint32(0, 0x06054b50, true)
  ev.setUint16(8, entries.length, true)
  ev.setUint16(10, entries.length, true)
  ev.setUint32(12, directory.length, true)
  ev.setUint32(16, offset, true)
  return concat([...chunks, directory, end])
}

function concat(parts: Uint8Array[]): Uint8Array<ArrayBuffer> {
  const out = new Uint8Array(parts.reduce((sum, part) => sum + part.length, 0))
  let at = 0
  for (const part of parts) {
    out.set(part, at)
    at += part.length
  }
  return out
}

function xlsx(parts: { shared?: string; sheet1: string; sheet2?: string }, deflate = false): ArrayBuffer {
  const entries: ZipInput[] = [
    {
      name: 'xl/workbook.xml',
      body: `<workbook xmlns:r="http://schemas.openxmlformats.org/officeDocument/2006/relationships"><sheets>
        <sheet name="明细" sheetId="1" r:id="rId1"/>${parts.sheet2 ? '<sheet name="Second" sheetId="2" r:id="rId2"/>' : ''}
      </sheets></workbook>`,
    },
    {
      name: 'xl/_rels/workbook.xml.rels',
      body: `<Relationships>
        <Relationship Id="rId1" Type="worksheet" Target="worksheets/sheet1.xml"/>
        ${parts.sheet2 ? '<Relationship Id="rId2" Type="worksheet" Target="worksheets/sheet2.xml"/>' : ''}
      </Relationships>`,
    },
    { name: 'xl/worksheets/sheet1.xml', body: parts.sheet1, deflate },
  ]
  if (parts.shared) entries.push({ name: 'xl/sharedStrings.xml', body: parts.shared, deflate })
  if (parts.sheet2) entries.push({ name: 'xl/worksheets/sheet2.xml', body: parts.sheet2 })
  const zipped = buildZip(entries)
  return zipped.buffer.slice(zipped.byteOffset, zipped.byteOffset + zipped.byteLength)
}

function prefixedXlsx(): ArrayBuffer {
  const zipped = buildZip([
    {
      name: 'xl/workbook.xml',
      body: '<x:workbook xmlns:x="urn:sheet" xmlns:r="urn:rels"><x:sheets><x:sheet name="询盘明细" sheetId="1" r:id="rId1" /></x:sheets></x:workbook>',
    },
    {
      name: 'xl/_rels/workbook.xml.rels',
      body: '<Relationships><Relationship Id="rId1" Type="worksheet" Target="/xl/worksheets/sheet1.xml" /></Relationships>',
    },
    {
      name: 'xl/worksheets/sheet1.xml',
      body: `<x:worksheet xmlns:x="urn:sheet"><x:sheetData>
        <x:row r="1"><x:c r="A1" t="inlineStr"><x:is><x:t>产品</x:t></x:is></x:c><x:c r="B1" t="inlineStr"><x:is><x:t>数量</x:t></x:is></x:c></x:row>
        <x:row r="2"><x:c r="A2" t="inlineStr"><x:is><x:t>冷轧卷</x:t></x:is></x:c><x:c r="B2"><x:v>10</x:v></x:c></x:row>
      </x:sheetData></x:worksheet>`,
    },
  ])
  return zipped.buffer.slice(zipped.byteOffset, zipped.byteOffset + zipped.byteLength)
}

// ------------------------------------------------------------------ tests

describe('isDirectTableFile', () => {
  it('accepts spreadsheet extensions and content types', () => {
    expect(isDirectTableFile('inquiry.xlsx')).toBe(true)
    expect(isDirectTableFile('QUOTE.CSV')).toBe(true)
    expect(isDirectTableFile('a.tsv')).toBe(true)
    expect(isDirectTableFile('no-extension', 'application/vnd.openxmlformats-officedocument.spreadsheetml.sheet')).toBe(true)
    expect(isDirectTableFile('file.bin', 'text/csv; charset=utf-8')).toBe(true)
  })
  it('rejects formats the reader does not understand', () => {
    expect(isDirectTableFile('legacy.xls')).toBe(false)
    expect(isDirectTableFile('scan.pdf')).toBe(false)
    expect(isDirectTableFile('photo.png', 'image/png')).toBe(false)
  })
})

describe('parseTableFile csv', () => {
  it('reads headers and rows with quoting, CRLF and BOM', async () => {
    const csv = '﻿产品,数量,备注\r\n"镀锌卷, 0.5mm","1,200","含""特殊""字符"\r\n第二行,3,\r\n\r\n'
    const book = await parseTableFile('询盘.csv', new TextEncoder().encode(csv).buffer as ArrayBuffer)
    expect(book.sheets).toHaveLength(1)
    expect(book.sheets[0].name).toBe('询盘')
    expect(book.sheets[0].columns).toEqual(['产品', '数量', '备注'])
    expect(book.sheets[0].rows[0]).toEqual(['镀锌卷, 0.5mm', '1,200', '含"特殊"字符'])
    expect(book.sheets[0].rows[1]).toEqual(['第二行', '3', ''])
    expect(book.sheets[0].totalRows).toBe(2)
  })

  it('honours the tab delimiter for tsv', async () => {
    const book = await parseTableFile('a.tsv', new TextEncoder().encode('a\tb\n1\t2\n').buffer as ArrayBuffer)
    expect(book.sheets[0].columns).toEqual(['a', 'b'])
    expect(book.sheets[0].rows).toEqual([['1', '2']])
  })
})

describe('parseTableFile xlsx', () => {
  it('reads valid OOXML whose spreadsheet elements use namespace prefixes', async () => {
    const book = await parseTableFile('prefixed.xlsx', prefixedXlsx())
    expect(book.sheets[0].name).toBe('询盘明细')
    expect(book.sheets[0].columns).toEqual(['产品', '数量'])
    expect(book.sheets[0].rows).toEqual([['冷轧卷', '10']])
  })

  it('reads shared strings, inline strings, numbers and sparse cells', async () => {
    const book = await parseTableFile('quote.xlsx', xlsx({
      shared: '<sst><si><t>产品</t></si><si><r><t>镀</t></r><r><t>锌卷</t></r></si><si><t xml:space="preserve">A&amp;B</t></si></sst>',
      sheet1: `<worksheet><sheetData>
        <row r="1"><c r="A1" t="s"><v>0</v></c><c r="B1" t="inlineStr"><is><t>数量</t></is></c><c r="C1" t="inlineStr"><is><t>标记</t></is></c></row>
        <row r="2"><c r="A2" t="s"><v>1</v></c><c r="B2"><v>1200.5</v></c><c r="C2" t="s"><v>2</v></c></row>
        <row r="3"><c r="B3" t="b"><v>1</v></c></row>
      </sheetData></worksheet>`,
    }))
    const [sheet] = book.sheets
    expect(sheet.name).toBe('明细')
    expect(sheet.columns).toEqual(['产品', '数量', '标记'])
    expect(sheet.rows[0]).toEqual(['镀锌卷', '1200.5', 'A&B'])
    expect(sheet.rows[1]).toEqual(['', '1', ''])
    expect(sheet.totalRows).toBe(2)
  })

  it('reads deflated members and every sheet', async () => {
    const book = await parseTableFile('multi.xlsx', xlsx({
      sheet1: '<worksheet><sheetData><row r="1"><c r="A1" t="inlineStr"><is><t>h</t></is></c></row><row r="2"><c r="A2" t="inlineStr"><is><t>v</t></is></c></row></sheetData></worksheet>',
      sheet2: '<worksheet><sheetData><row r="1"><c r="A1" t="inlineStr"><is><t>two</t></is></c></row><row r="2"><c r="A2" t="inlineStr"><is><t>x</t></is></c></row></sheetData></worksheet>',
    }, true))
    expect(book.sheets.map((sheet) => sheet.name)).toEqual(['明细', 'Second'])
    expect(book.sheets[1].columns).toEqual(['two'])
    expect(book.sheets[1].rows).toEqual([['x']])
  })

  it('caps the preview at two hundred rows but counts them all', async () => {
    const rows = Array.from({ length: 250 }, (_, i) => `<row r="${i + 2}"><c r="A${i + 2}"><v>${i}</v></c></row>`).join('')
    const book = await parseTableFile('big.xlsx', xlsx({
      sheet1: `<worksheet><sheetData><row r="1"><c r="A1" t="inlineStr"><is><t>n</t></is></c></row>${rows}</sheetData></worksheet>`,
    }))
    expect(book.sheets[0].rows).toHaveLength(200)
    expect(book.sheets[0].totalRows).toBe(250)
  })

  it('refuses files that are not workbooks', async () => {
    await expect(parseTableFile('fake.xlsx', new TextEncoder().encode('not a zip').buffer as ArrayBuffer)).rejects.toThrow()
  })
})
