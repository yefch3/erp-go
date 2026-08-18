import { describe, expect, it } from 'vitest'
import { moveField, nextCustomFieldKey, validateTemplateFields, type TemplateField } from './inquiryTemplates'

function field(partial: Partial<TemplateField>): TemplateField {
  return {
    fieldKey: 'product', displayName: '产品', sortOrder: 1, isRequired: false,
    defaultValue: '', dataType: 'TEXT', isCustom: false, isCore: false, ...partial,
  }
}

function validFields(): TemplateField[] {
  return [
    field({ fieldKey: 'product', displayName: '品名', isCore: true, isRequired: true }),
    field({ fieldKey: 'quantity', displayName: '需求数量', isCore: true, isRequired: true, sortOrder: 2 }),
    field({ fieldKey: 'quantity_unit', displayName: '计量单位', isCore: true, isRequired: true, sortOrder: 3 }),
    field({ fieldKey: 'unit_price', displayName: '单价', isCore: true, sortOrder: 4 }),
    field({ fieldKey: 'total_price', displayName: '总价', isCore: true, sortOrder: 5 }),
    field({ fieldKey: 'custom.customer_part_no', displayName: '客户料号', isCustom: true, sortOrder: 6 }),
  ]
}

describe('moveField', () => {
  it('swaps neighbours and renumbers sort order', () => {
    const fields = validFields()
    moveField(fields, 1, -1)
    expect(fields.map((f) => f.fieldKey.slice(0, 8))).toEqual(['quantity', 'product', 'quantity', 'unit_pri', 'total_pr', 'custom.c'])
    expect(fields.map((f) => f.sortOrder)).toEqual([1, 2, 3, 4, 5, 6])
  })
  it('does nothing at the edges', () => {
    const fields = validFields()
    moveField(fields, 0, -1)
    moveField(fields, 5, 1)
    expect(fields.map((f) => f.sortOrder)).toEqual([1, 2, 3, 4, 5, 6])
  })
})

describe('nextCustomFieldKey', () => {
  it('picks the first free column_N', () => {
    expect(nextCustomFieldKey([])).toBe('custom.column_1')
    expect(nextCustomFieldKey(validFields())).toBe('custom.column_1')
    expect(nextCustomFieldKey([field({ fieldKey: 'custom.column_1', isCustom: true })])).toBe('custom.column_2')
  })
})

describe('validateTemplateFields', () => {
  it('accepts a valid field set', () => {
    expect(validateTemplateFields(validFields())).toBeNull()
  })
  it('requires every core field including the price columns', () => {
    expect(validateTemplateFields(validFields().filter((f) => f.fieldKey !== 'quantity'))).toBe('inquiryTemplates.errors.coreMissing')
    expect(validateTemplateFields(validFields().filter((f) => f.fieldKey !== 'unit_price'))).toBe('inquiryTemplates.errors.coreMissing')
    expect(validateTemplateFields(validFields().filter((f) => f.fieldKey !== 'total_price'))).toBe('inquiryTemplates.errors.coreMissing')
  })
  it('forbids required price columns', () => {
    const fields = validFields()
    fields[3].isRequired = true
    expect(validateTemplateFields(fields)).toBe('inquiryTemplates.errors.priceRequired')
  })
  it('rejects duplicate headers case-insensitively', () => {
    const fields = validFields()
    fields[5].displayName = '品名'
    expect(validateTemplateFields(fields)).toBe('inquiryTemplates.errors.nameDuplicate')
  })
  it('rejects a malformed custom key', () => {
    const fields = validFields()
    fields[5].fieldKey = 'custom.客户料号'
    expect(validateTemplateFields(fields)).toBe('inquiryTemplates.errors.keyInvalid')
  })
})
