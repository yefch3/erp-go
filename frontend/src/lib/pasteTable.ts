// Turning a block pasted out of a spreadsheet into rows.
//
// Paste rather than file upload, for one reason that decides it: Excel on
// Chinese Windows saves CSV in GBK, not UTF-8, and a file upload would spend
// the rest of its life mis-decoding 姓名 into mojibake for the people most
// likely to use this. The clipboard hands the browser text that is already
// decoded, so the whole problem never exists.

/** One recognised column, and the words a real spreadsheet heads it with. */
const COLUMNS = [
  { field: 'code', aliases: ['工号', '员工编号', '编号', 'code', 'employee no', 'employee id'] },
  { field: 'name', aliases: ['姓名', '名字', 'name'] },
  { field: 'department', aliases: ['部门', 'department', 'dept'] },
  { field: 'position', aliases: ['岗位', '职位', 'position', 'title'] },
  { field: 'email', aliases: ['邮箱', '邮件', '电子邮箱', 'email', 'e-mail'] },
  { field: 'phone', aliases: ['电话', '手机', '手机号', 'phone', 'mobile'] },
  { field: 'managerCode', aliases: ['直属上级', '上级', '上级工号', 'manager', 'manager code'] },
] as const

export type ImportRow = Record<(typeof COLUMNS)[number]['field'], string>

/** The order assumed when the block has no header row. */
const DEFAULT_ORDER = COLUMNS.map((c) => c.field)

const EMPTY: ImportRow = {
  code: '', name: '', department: '', position: '', email: '', phone: '', managerCode: '',
}

function normalise(cell: string): string {
  // Non-breaking spaces arrive from anything that passed through a web page,
  // and full-width spaces from Chinese input methods. Neither is visible, and
  // both would otherwise become part of a 工号.
  return cell.replace(/[ 　]/g, ' ').trim()
}

function splitCells(line: string): string[] {
  // Tabs when the source was a spreadsheet, commas when it was a CSV opened
  // in a text editor. Checked in that order because a name may legitimately
  // contain a comma and never contains a tab.
  return (line.includes('\t') ? line.split('\t') : line.split(',')).map(normalise)
}

/**
 * matchHeader maps a first row of column names onto fields, or returns null
 * when the row is data rather than a header.
 *
 * Guessing wrong in either direction is bad in a specific way: treating a
 * header as data creates an employee called 姓名, and treating data as a
 * header silently drops a real person. So the test is deliberately strict —
 * at least two cells have to be recognised names.
 */
function matchHeader(cells: string[]): string[] | null {
  const mapped = cells.map((cell) => {
    const key = cell.toLowerCase().replace(/[\s*：:]/g, '')
    const hit = COLUMNS.find((c) => c.aliases.some((a) => key === a.replace(/\s/g, '')))
    return hit ? hit.field : ''
  })
  return mapped.filter(Boolean).length >= 2 ? mapped : null
}

export interface ParsedPaste {
  rows: ImportRow[]
  /** True when a header row was recognised and skipped. */
  usedHeader: boolean
}

/**
 * parsePaste turns pasted text into rows. It never rejects anything: every
 * judgement about whether a row means something belongs to the server, which
 * is the only place that knows what departments exist and which addresses are
 * taken. Parsing here decides only where one cell ends and the next begins.
 */
export function parsePaste(text: string): ParsedPaste {
  const lines = text
    .split(/\r?\n/)
    .filter((l) => l.trim() !== '')
  if (lines.length === 0) return { rows: [], usedHeader: false }

  const first = splitCells(lines[0])
  const header = matchHeader(first)
  const order = header ?? DEFAULT_ORDER
  const body = header ? lines.slice(1) : lines

  const rows = body.map((line) => {
    const cells = splitCells(line)
    const row: ImportRow = { ...EMPTY }
    order.forEach((field, i) => {
      if (field && field in row) row[field as keyof ImportRow] = cells[i] ?? ''
    })
    return row
  })
  return { rows, usedHeader: header !== null }
}
