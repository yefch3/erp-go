import { readFileSync } from 'node:fs'
import { describe, expect, it } from 'vitest'
import { parseCustomerImportWorkbook } from './customerImportWorkbook'
import { autoMapCustomerHeaders, buildCustomerImportRows, CUSTOMER_TEMPLATE_HEADERS, customerTemplateProblems, fixedCustomerTemplateMapping } from './customerImport'
import './testsupport/deflateRaw'

const bytes = (path: string) => new Uint8Array(readFileSync(path)).buffer
describe('customer template files', () => {
  it('accepts fixed headers but rejects renamed, reordered, extra or missing columns', () => {
    expect(customerTemplateProblems(CUSTOMER_TEMPLATE_HEADERS)).toEqual([])
    expect(customerTemplateProblems(CUSTOMER_TEMPLATE_HEADERS.filter(h => h !== '客户简称').map(h => `${h} `))).toEqual([])
    const changed = [...CUSTOMER_TEMPLATE_HEADERS]; [changed[0], changed[1]] = [changed[1], changed[0]]
    for (const headers of [changed, [...CUSTOMER_TEMPLATE_HEADERS, '自定义字段'], CUSTOMER_TEMPLATE_HEADERS.map(h => h === '公司电话' ? '联系人电话' : h), CUSTOMER_TEMPLATE_HEADERS.slice(1)]) {
      expect(customerTemplateProblems(headers)).toHaveLength(1)
      expect(fixedCustomerTemplateMapping(headers)).toEqual({})
    }
  })
  it('downloads a blank 18-column workbook with the approved headers', async () => {
    const workbook = await parseCustomerImportWorkbook('customer-import.xlsx', bytes('public/templates/customer-import.xlsx'))
    expect(workbook.sheets[0].columns).toEqual(CUSTOMER_TEMPLATE_HEADERS)
    expect(buildCustomerImportRows(workbook.sheets[0].rows, autoMapCustomerHeaders(CUSTOMER_TEMPLATE_HEADERS))).toEqual([])
  })
  it.runIf(!!process.env.CUSTOMER_XLS_SAMPLE)('reads the original legacy xls with blank padded rows', async () => {
    const workbook = await parseCustomerImportWorkbook('客户资料.xls', bytes(process.env.CUSTOMER_XLS_SAMPLE!))
    expect(workbook.sheets[0].columns.map(h => h.trim())).toEqual(CUSTOMER_TEMPLATE_HEADERS.filter(h => h !== '客户简称'))
    expect(buildCustomerImportRows(workbook.sheets[0].rows, autoMapCustomerHeaders(workbook.sheets[0].columns))).toEqual([])
  })
  it('separates company and contact data, preserves text codes, resolves multiple owners', () => {
    const mapping = autoMapCustomerHeaders(['客户名称','邮政编码','公司电话','电子信箱','联系人邮箱','分管人'])
    const rows = buildCustomerImportRows([['Example','001010','01012345678','office@example.com','person@example.com','销售A；销售B']], mapping, [{id:1,name:'销售A'},{id:2,name:'销售B'}])
    expect(rows[0]).toMatchObject({postalCode:'001010',companyPhone:'01012345678',companyEmail:'office@example.com',contactEmail:'person@example.com',ownerEmployeeIds:['1','2']})
  })
})
