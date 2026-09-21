import { parseCustomerImportWorkbook } from './customerImportWorkbook'

export const supplierImportHeaders = [
  '供应商编码', '中文名称', '英文名称', '供应商简称', '国家/地区代码', '默认币种', '付款方式',
  '业务角色（多个用分号）', '税号', '注册地址', '联系地址', '供应商备注', '联系人姓名',
  '联系人部门', '联系人职位', '联系人电话', '联系人邮箱', '主要联系人（是/否）', '联系人备注',
] as const

const keys = [
  'code', 'nameZh', 'nameEn', 'shortName', 'countryCode', 'currency', 'paymentTerm', 'businessTypes',
  'taxId', 'registeredAddress', 'address', 'remark', 'contactName', 'contactDepartment', 'contactTitle',
  'contactPhone', 'contactEmail', 'contactIsPrimary', 'contactRemark',
] as const

export async function parseSupplierImportWorkbook(name: string, data: ArrayBuffer): Promise<Record<string, unknown>[]> {
  const workbook = await parseCustomerImportWorkbook(name, data)
  if (workbook.sheets.length !== 1) throw new Error('只允许一个工作表')
  const sheet = workbook.sheets[0]
  if (sheet.columns.length !== supplierImportHeaders.length || sheet.columns.some((value, index) => value.trim() !== supplierImportHeaders[index])) {
    throw new Error('请使用系统下载的固定供应商模板，不能修改列名或顺序')
  }
  return sheet.rows
    .map((cells, index) => ({ cells, rowNumber: index + 2 }))
    .filter(({ cells }) => cells.some(value => value.trim()))
    .map(({ cells, rowNumber }) => {
      const row: Record<string, unknown> = { rowNumber }
      keys.forEach((key, index) => {
        const value = String(cells[index] ?? '').trim()
        if (key === 'businessTypes') row[key] = value.split(/[|;；、,，]/).map(item => item.trim()).filter(Boolean)
        else if (key === 'contactIsPrimary') row[key] = ['是', 'yes', 'true', '1', 'y'].includes(value.toLowerCase())
        else row[key] = value
      })
      return row
    })
}
