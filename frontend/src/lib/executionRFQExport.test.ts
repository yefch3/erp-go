import { describe, expect, it } from 'vitest'
import { executionRFQExportRows } from './executionRFQExport'

describe('execution inquiry workbook preview', () => {
  it('prepares each pending specification independently and excludes known internal fields', () => {
    const base = { id: '101', productName: '钢卷', uomCode: 'MT', availableQty: '17.125' }
    const inputs = [
      { ...base, spec: '1.0 × 1250 mm; 材质/标准：JIS G3141；包装要求：防潮包装; 客户名称：敏感客户; 外销单价: 90; 利润: 15; Remarks: 内部自由文本', contractItemRemark: '内部合同明细' },
      { ...base, id: '102', spec: 'JIS G3141 SPCC 2 × 1250 mm; Surface requirement: Oiled', availableQty: '25' },
      { ...base, id: '103', spec: '3 × 1250 mm', availableQty: '0' },
    ]
    const rows = executionRFQExportRows(inputs)
    expect(rows.map(row => row.line)).toEqual([
      { requirement_id: '101', available_qty: '17.125', product_name: '钢卷', material_standard: 'JIS G3141', specification: '1.0 × 1250 mm', technical_requirements: '包装要求：防潮包装' },
      { requirement_id: '102', available_qty: '25', product_name: '钢卷', material_standard: '', specification: 'JIS G3141 SPCC 2 × 1250 mm', technical_requirements: 'Surface requirement: Oiled' },
    ])
    expect(rows[0].sourceRemark).toBe('内部合同明细')
  })
})
