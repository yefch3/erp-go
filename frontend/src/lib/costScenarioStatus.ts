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

// 成本方案状态和客户报价状态来自两个服务；页面在这里合并成采购员真正关心的阶段。
export function describeCostScenario(
  scenario: CostScenarioStatusInput,
  quotation?: CustomerQuotationStatusInput,
): CostScenarioPresentation {
  if (scenario.status === 'SUPERSEDED') return { label: '已失效', type: 'info' }
  if (scenario.status === 'DRAFT') return { label: '草稿', type: 'info' }
  if (scenario.status === 'CONFIRMED') return { label: '已确认', type: 'success' }

  if (scenario.status === 'CUSTOMER_QUOTE_CREATED' || Number(scenario.customerQuotationId || 0) > 0) {
    if (quotation?.status === 'SENT') return { label: '等待客户确认', type: 'warning' }
    if (quotation?.status === 'ACCEPTED') return { label: '客户已接受', type: 'success' }
    if (quotation?.status === 'REJECTED') return { label: '客户已拒绝', type: 'danger' }
    if (quotation?.status === 'EXPIRED') return { label: '报价已过期', type: 'info' }
    if (quotation?.status === 'CANCELLED') return { label: '报价已取消', type: 'info' }
    return { label: '已生成客户报价', type: 'primary' }
  }

  return { label: scenario.status || '未知', type: 'info' }
}
