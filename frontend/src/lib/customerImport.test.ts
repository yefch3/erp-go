import { describe, expect, it } from 'vitest'
import { autoMapCustomerHeaders, buildCustomerImportFieldMappings, buildCustomerImportRows, customerImportMappingProblems, extensionCustomerFields } from './customerImport'

describe('customer import column mapping', () => {
  it('matches reordered Chinese and English headers', () => {
    expect(autoMapCustomerHeaders(['Email', '客户名称', '国家代码', '电话'])).toEqual({
      0: 'system:contactEmail', 1: 'system:name', 2: 'system:countryCode', 3: 'system:contactPhone',
    })
  })

  it('does not map the same system field twice', () => {
    expect(autoMapCustomerHeaders(['名称', 'Customer Name'])).toEqual({ 0: 'system:name', 1: 'new:1' })
  })

  it('builds rows by mapping and removes empty lines', () => {
    const rows = buildCustomerImportRows(
      [['Amy', 'ACME'], ['', '']],
      { 0: 'system:contactName', 1: 'system:name' },
    )
    expect(rows).toEqual([{ contactName: 'Amy', name: 'ACME' }])
  })

  it('requires a customer name column', () => {
    expect(customerImportMappingProblems({ 0: 'system:code' })).toEqual(['请选择一列作为“客户名称”'])
  })

  it('matches an existing dynamic field and prepares a new field', () => {
    const fields = [{ fieldKey: 'custom_region', displayName: '销售区域', aliases: ['区域'] }]
    const mapping = autoMapCustomerHeaders(['名称', '区域', '展会偏好'], fields)
    expect(mapping).toEqual({ 0: 'system:name', 1: 'custom:custom_region', 2: 'new:2' })
    expect(buildCustomerImportFieldMappings(['名称', '区域', '展会偏好'], mapping, { 2: '展会偏好' }, fields)).toEqual([
      { sourceKey: 'column_1', fieldKey: 'custom_region', displayName: '销售区域', aliases: ['区域'] },
      { sourceKey: 'column_2', fieldKey: '', displayName: '展会偏好', aliases: ['展会偏好'] },
    ])
  })

  it('routes known legacy customer columns into their owning modules', () => {
    const headers = ['客户代码', '客户名称', '所属地区', '通信地址', '邮政编码', '客户等级', '分管人', '联系人姓名', '联系人邮箱', '联系人电话', '联系人手机']
    const mapping = autoMapCustomerHeaders(headers)
    expect(mapping).toEqual({
      0: 'system:code', 1: 'system:name', 2: 'system:countryRegion', 3: 'system:address',
      4: 'system:postalCode', 5: 'system:creditGrade', 6: 'system:ownerName', 7: 'system:contactName',
      8: 'system:contactEmail', 9: 'system:contactPhone', 10: 'system:contactMobile',
    })
    expect(buildCustomerImportRows([
      ['TEST-1', '测试客户', '美国·加利福尼亚州', '100 Test Street', '94105', 'B', '系统管理员', 'Amy', 'amy@example.com', '+1-415-555-0103', '+1-415-555-0104'],
    ], mapping, [{ id: 9, name: '系统管理员' }])).toEqual([{
      code: 'TEST-1', name: '测试客户', countryCode: 'US', addressState: '加利福尼亚州', address: '100 Test Street', postalCode: '94105',
      creditGrade: 'B', ownerName: '系统管理员', ownerEmployeeId: '9', contactName: 'Amy', contactEmail: 'amy@example.com',
      contactPhone: '+1-415-555-0103', contactMobile: '+1-415-555-0104',
    }])
  })
  it('keeps canonical customer fields out of Excel extension fields', () => {
    expect(extensionCustomerFields([
      { fieldKey: 'custom_code', displayName: '客户代码' },
      { fieldKey: 'custom_contact', displayName: '旧联系人', aliases: ['联系人姓名'] },
      { fieldKey: 'custom_preference', displayName: '采购偏好' },
    ])).toEqual([{ fieldKey: 'custom_preference', displayName: '采购偏好' }])
  })
})
