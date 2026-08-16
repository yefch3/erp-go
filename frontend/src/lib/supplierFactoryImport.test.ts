import { describe, expect, it } from 'vitest'
import { importTemplate, parseImportCSV } from './supplierFactoryImport'

describe('supplier/factory CSV import', () => {
  it('parses supplier business types and quoted commas', () => {
    const csv = `${importTemplate('supplier')}SUP-1,深圳供应商,Shenzhen Supplier,,CN,USD,30 days,CARRIER|FORWARDER,张三,,a@example.com,"深圳,南山",备注`
    const rows = parseImportCSV(csv, 'supplier')
    expect(rows[0]).toMatchObject({ code: 'SUP-1', countryCode: 'CN', businessTypes: ['CARRIER', 'FORWARDER'], address: '深圳,南山' })
  })

  it('rejects a reordered template', () => {
    expect(() => parseImportCSV('name_zh,code\n工厂,F-1', 'factory')).toThrow('HEADER')
  })
})
