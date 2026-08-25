import { describe, expect, it } from 'vitest'
import { describeCostScenario } from './costScenarioStatus'

describe('describeCostScenario', () => {
  it('区分草稿、确认、生成报价和失效版本', () => {
    expect(describeCostScenario({ status: 'DRAFT' }).label).toBe('草稿')
    expect(describeCostScenario({ status: 'CONFIRMED' }).label).toBe('已确认')
    expect(describeCostScenario({ status: 'CUSTOMER_QUOTE_CREATED', customerQuotationId: 8 }).label)
      .toBe('已生成客户报价')
    expect(describeCostScenario({ status: 'SUPERSEDED' }).label).toBe('已失效')
  })

  it('把客户拒绝反馈显示在对应成本版本上', () => {
    expect(describeCostScenario(
      { status: 'CUSTOMER_QUOTE_CREATED', customerQuotationId: 8 },
      { status: 'REJECTED' },
    )).toEqual({ label: '客户已拒绝', type: 'danger' })
  })

  it('客户接受后明确显示为已接受', () => {
    expect(describeCostScenario(
      { status: 'CUSTOMER_QUOTE_CREATED', customerQuotationId: 9 },
      { status: 'ACCEPTED' },
    )).toEqual({ label: '客户已接受', type: 'success' })
  })
})
