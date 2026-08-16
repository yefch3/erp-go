import { describe, expect, it } from 'vitest'

import {
  businessRoleLabel,
  certificateStatusLabel,
  changeSectionLabel,
  changeSummaryLabel,
  operatorDisplayName,
} from './masterDataDisplay'

const translations: Record<string, string> = {
  'suppliers.businessRoles.CARRIER': 'Carrier',
  'suppliers.certificateStatus.VALID': 'Valid',
  'suppliers.changeSections.PROFILE': 'Profile',
  'suppliers.changeMessages.factoryCreated': 'Factory created',
  'suppliers.changeMessages.ownerAdded': 'Internal owner added',
  'suppliers.changeMessages.factorySuspendedBySupplier': 'Factory suspended because its supplier was deactivated',
  'suppliers.systemAdministrator': 'System Administrator',
}

const t = (key: string) => translations[key] || key

describe('master data display labels', () => {
  it('translates stable business and certificate codes', () => {
    expect(businessRoleLabel('CARRIER', t)).toBe('Carrier')
    expect(certificateStatusLabel('VALID', t)).toBe('Valid')
    expect(changeSectionLabel('PROFILE', t)).toBe('Profile')
  })

  it('translates known change events and preserves user-entered details', () => {
    expect(changeSummaryLabel({ action: 'CREATE', section: 'PROFILE' }, 'factory', t)).toBe('Factory created')
    expect(
      changeSummaryLabel({ action: 'CREATE', section: 'OWNER', summary: '新增负责人：测试员工3' }, 'supplier', t),
    ).toBe('Internal owner added: 测试员工3')
    expect(changeSummaryLabel({ action: 'CASCADE', section: 'PROFILE' }, 'factory', t)).toBe(
      'Factory suspended because its supplier was deactivated',
    )
  })

  it('translates only the built-in administrator name', () => {
    expect(operatorDisplayName('系统管理员', t)).toBe('System Administrator')
    expect(operatorDisplayName('Alice Chen', t)).toBe('Alice Chen')
  })
})
