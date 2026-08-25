export type CostScenarioTagType = 'primary' | 'success' | 'warning' | 'danger' | 'info'

export interface CostScenarioStatusInput {
  status: string
  customerQuotationId?: string | number
}

export interface CustomerQuotationStatusInput {
  status?: string
}

export interface CostScenarioPresentation {
  label: string
  type: CostScenarioTagType
}

// 成本方案只表达内部核价结论，不混入客户报价单的生命周期。
export function describeCostScenario(scenario: CostScenarioStatusInput): CostScenarioPresentation {
  if (scenario.status === 'SUPERSEDED') return { label: '已失效', type: 'info' }
  if (scenario.status === 'DRAFT') return { label: '草稿', type: 'info' }
  if (scenario.status === 'CONFIRMED') return { label: '已确认', type: 'success' }

  return { label: scenario.status || '未知', type: 'info' }
}

// 客户报价单独展示，避免采购员把“成本已确认”误解成“客户已接受”。
export function describeCustomerQuotation(
  quotation?: CustomerQuotationStatusInput,
): CostScenarioPresentation {
  if (!quotation) return { label: '尚未生成', type: 'info' }
  if (quotation.status === 'DRAFT') return { label: '报价草稿', type: 'primary' }
  if (quotation.status === 'SENT') return { label: '等待客户确认', type: 'warning' }
  if (quotation.status === 'ACCEPTED') return { label: '客户已接受', type: 'success' }
  if (quotation.status === 'REJECTED') return { label: '客户已拒绝', type: 'danger' }
  if (quotation.status === 'EXPIRED') return { label: '报价已过期', type: 'info' }
  if (quotation.status === 'CANCELLED') return { label: '报价已取消', type: 'info' }
  return { label: quotation.status || '已生成', type: 'primary' }
}
