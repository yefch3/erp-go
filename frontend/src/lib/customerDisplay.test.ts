import { describe, expect, it } from 'vitest'
import { customerDisplayName, customerOptionLabel } from './customerDisplay'

describe('customer display helpers', () => {
  it('uses the short name on business screens', () => {
    expect(customerDisplayName({ name: '上海示例进出口有限公司', shortName: '上海示例' })).toBe('上海示例')
  })

  it('falls back to the registered customer name', () => {
    expect(customerDisplayName({ name: '上海示例进出口有限公司', shortName: '  ' })).toBe('上海示例进出口有限公司')
  })

  it('keeps the full name visible in customer option details', () => {
    expect(customerOptionLabel({ code: 'CU-001', name: '上海示例进出口有限公司', shortName: '上海示例' }))
      .toBe('CU-001 · 上海示例（上海示例进出口有限公司）')
  })
})
