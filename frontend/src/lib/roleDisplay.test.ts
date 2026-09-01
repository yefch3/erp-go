import { describe, expect, it } from 'vitest'
import { permissionDisplayName, roleDisplayDescription, roleDisplayName } from './roleDisplay'

describe('role display localization', () => {
  it('localizes preset roles and descriptions', () => {
    expect(roleDisplayName('SHIPPING_MANAGER', '船运经理', 'en')).toBe('Shipping manager')
    expect(roleDisplayName('FINANCE', '财务', 'es')).toBe('Finanzas')
    expect(roleDisplayDescription('LOGISTICS', '仓储与发运', 'en')).toContain('Warehousing')
  })

  it('keeps custom role names untouched', () => {
    expect(roleDisplayName('CUSTOM', 'Regional operator', 'es')).toBe('Regional operator')
  })

  it('localizes permission labels from stable permission codes', () => {
    expect(permissionDisplayName('export:contract:read', '查看合同', 'en')).toBe('View contracts')
    expect(permissionDisplayName('procurement:order:cancel', '取消采购单', 'es')).toBe('Cancelar órdenes de compra')
    expect(permissionDisplayName('shipping:document:upload', '上传船期单证', 'en')).toBe('Upload shipping documents')
  })

  it('uses the server label for Chinese', () => {
    expect(permissionDisplayName('export:contract:read', '查看合同', 'zh')).toBe('查看合同')
  })
})
