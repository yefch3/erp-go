import { readFileSync } from 'node:fs'
import { fileURLToPath } from 'node:url'
import { describe, expect, it } from 'vitest'
import {
  excelPollAfterFailure,
  excelPollGiveUpAfter,
  hydrateExcelResult,
  serverMessageOf,
  type ExcelResult,
} from './excelJobResult'
import './testsupport/deflateRaw'

// 这份文件是邮件服务的 Go 代码真写出来的——services/mail/internal/app/
// excel_metadata_test.go 里的 TestExcelJobFixtureMatchesWriter 负责生成，也
// 负责在写文件的代码变了之后把它标红。三行询盘，第二行有单价，所以总价
// 列是一个公式格：<f>T3*U3</f><v>31025</v>。
//
// 预览的行就是从这样的文件里解出来的，所以「Go 写的」和「这里读的」得是
// 同一份东西——这条测试钉住后一半。
const fixture = readFileSync(fileURLToPath(new URL('./testdata/excel-job-result.xlsx', import.meta.url)))

function delivered(overrides: Partial<ExcelResult> = {}): ExcelResult {
  return {
    fileName: '询盘.xlsx',
    fileData: fixture.toString('base64'),
    model: 'test',
    sheets: [
      {
        name: '询盘',
        summary: '三条询盘，一条带单价',
        columns: [],
        columnKeys: ['product'],
        rows: [],
        totalRows: '0',
      },
    ],
    ...overrides,
  }
}

describe('hydrateExcelResult', () => {
  it('旧任务自带行，原样返回', async () => {
    const legacy = delivered({
      sheets: [{ name: '询盘', summary: '', columns: ['产品'], rows: [{ cells: ['A'] }], totalRows: '1' }],
    })
    expect(await hydrateExcelResult(legacy)).toBe(legacy)
  })

  it('行从文件里解出来，公式格给的是算好的数', async () => {
    const input = delivered()
    const out = await hydrateExcelResult(input)
    const sheet = out.sheets[0]
    expect(sheet.rows).toHaveLength(3)
    expect(sheet.totalRows).toBe('3')
    expect(sheet.columns).toHaveLength(22)
    expect(sheet.columns[0]).toBe('产品')
    expect(sheet.rows[0].cells[0]).toBe('热轧钢卷')
    expect(sheet.rows[0].cells[19]).toBe('100')
    expect(sheet.rows[1].cells[0]).toBe('冷轧钢卷 <A&B>')
    // 总价列（第 22 列）：50 × 620.5。人看到的是数，不是「=T3*U3」。
    expect(sheet.rows[1].cells[21]).toBe('31025')
    expect(sheet.rows[0].cells[21]).toBe('')
    for (const row of sheet.rows) for (const cell of row.cells) expect(cell.startsWith('=')).toBe(false)
    // 文件里没有的那几样，从 metadata 里原样带过来。
    expect(sheet.summary).toBe('三条询盘，一条带单价')
    expect(sheet.columnKeys).toEqual(['product'])
    // 下载按钮还要用文件本身。
    expect(out.fileData).toBe(input.fileData)
    // 不动传进来的那份。
    expect(out).not.toBe(input)
    expect(input.sheets[0].rows).toEqual([])
  })

  it('metadata 里有表头就用 metadata 的', async () => {
    const out = await hydrateExcelResult(delivered({ sheets: [{ ...delivered().sheets[0], columns: ['A'] }] }))
    expect(out.sheets[0].columns).toEqual(['A'])
  })

  it('文件里的表比 metadata 少，是错，不是空表', async () => {
    const two = delivered({ sheets: [delivered().sheets[0], { ...delivered().sheets[0], name: '第二张' }] })
    await expect(hydrateExcelResult(two)).rejects.toThrow(/sheets/)
  })

  it('文件坏了是错，不是空表', async () => {
    await expect(hydrateExcelResult(delivered({ fileData: btoa('not a workbook') }))).rejects.toThrow()
  })
})

describe('问任务结果失败之后', () => {
  it('一分钟之内接着问，到点就停——不再无限转圈', () => {
    expect(excelPollAfterFailure(0)).toBe('retry')
    expect(excelPollAfterFailure(excelPollGiveUpAfter - 1)).toBe('retry')
    expect(excelPollAfterFailure(excelPollGiveUpAfter)).toBe('stop')
    // 3 秒一次，上限就是约一分钟。
    expect(excelPollGiveUpAfter * 3).toBeGreaterThanOrEqual(60)
    expect(excelPollGiveUpAfter * 3).toBeLessThanOrEqual(90)
  })

  it('弹给人看的是服务端那句话；网络错误的英文不算', () => {
    expect(serverMessageOf({ success: false, code: 'MAIL_EXCEL_RESULT_UNREACHABLE', message: '暂时取不到' })).toBe('暂时取不到')
    expect(serverMessageOf({ success: false, code: 'X', message: '' })).toBeUndefined()
    expect(serverMessageOf(new Error('Network Error'))).toBeUndefined()
    expect(serverMessageOf(undefined)).toBeUndefined()
    expect(serverMessageOf('boom')).toBeUndefined()
  })
})
