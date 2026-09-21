import { readFileSync } from 'node:fs'
import { describe, expect, it } from 'vitest'
import { parseSupplierImportWorkbook, supplierImportHeaders } from './supplierImportWorkbook'
import { parseCustomerImportWorkbook } from './customerImportWorkbook'
import * as XLSX from 'xlsx'
import './testsupport/deflateRaw'

const templateBytes = () => new Uint8Array(readFileSync('public/templates/supplier-import.xlsx')).buffer

describe('supplier fixed import workbook', () => {
  it('ships one blank sheet with the exact basic and contact columns', async () => {
    const workbook = await parseCustomerImportWorkbook('supplier-import.xlsx', templateBytes())
    expect(workbook.sheets).toHaveLength(1)
    expect(workbook.sheets[0].columns).toEqual(supplierImportHeaders)
    expect(await parseSupplierImportWorkbook('supplier-import.xlsx', templateBytes())).toEqual([])
  })
  it('keeps repeated supplier rows so every contact can be imported', async () => {
    const workbook = XLSX.utils.book_new()
    XLSX.utils.book_append_sheet(workbook, XLSX.utils.aoa_to_sheet([
      [...supplierImportHeaders],
      ['SUP-1','供应商一','','简称','CN','CNY','月结',['GENERAL'],'','','','','张三','采购部','经理','13800138000','a@example.com','是','主要联系人'].flat(),
      ['SUP-1','','','','','','',['GENERAL'],'','','','','李四','财务部','','021-12345678','b@example.com','否',''].flat(),
    ]), '供应商导入')
    const data = XLSX.write(workbook, { bookType: 'xlsx', type: 'array' }) as ArrayBuffer
    const rows = await parseSupplierImportWorkbook('supplier-import.xlsx', data)
    expect(rows).toHaveLength(2)
    expect(rows[0]).toMatchObject({ code: 'SUP-1', contactName: '张三', contactDepartment: '采购部', contactIsPrimary: true, businessTypes: ['GENERAL'] })
    expect(rows[1]).toMatchObject({ code: 'SUP-1', contactName: '李四', contactIsPrimary: false })
  })
})
