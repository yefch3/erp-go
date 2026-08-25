import { describe, expect, it } from 'vitest'
import { describeCostScenario, describeCustomerQuotation } from './costScenarioStatus'

describe('describeCostScenario', () => {
  it('成本状态只区分草稿、确认和失效版本', () => {
    expect(describeCostScenario({ status: 'DRAFT' }).label).toBe('草稿')
    expect(describeCostScenario({ status: 'CONFIRMED' }).label).toBe('已确认')
    expect(describeCostScenario({ status: 'SUPERSEDED' }).label).toBe('已失效')
  })

  it('客户报价状态在独立列显示拒绝结果', () => {
    expect(describeCustomerQuotation({ status: 'REJECTED' }))
      .toEqual({ label: '客户已拒绝', type: 'danger' })
  })

  it('客户报价区分未生成、等待和已接受', () => {
    expect(describeCustomerQuotation().label).toBe('尚未生成')
    expect(describeCustomerQuotation({ status: 'SENT' }).label).toBe('等待客户确认')
    expect(describeCustomerQuotation({ status: 'ACCEPTED' }))
      .toEqual({ label: '客户已接受', type: 'success' })
  })
})
