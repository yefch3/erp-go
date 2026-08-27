export interface ApprovalBusinessRef {
  bizType: string
  bizId: string
}

export interface ApprovalSourceLink {
  path: string
  query: Record<string, string>
}

export interface ApprovalTimeAmount {
  overdue: boolean
  unit: 'days' | 'hours' | 'minutes'
  count: number
}

// 紧急颜色与剩余时间的单位选择保持为纯函数，便于三语页面共享和测试。
export function approvalPriorityTagType(code: string): 'danger' | 'warning' | 'info' {
  if (code === 'OVERDUE' || code === 'URGENT') return 'danger'
  return code === 'HIGH' ? 'warning' : 'info'
}

export function approvalTimeAmount(rawMinutes: string | number): ApprovalTimeAmount {
  const minutes = Number(rawMinutes)
  const absolute = Math.abs(Number.isFinite(minutes) ? minutes : 0)
  if (absolute >= 1440) return { overdue: minutes <= 0, unit: 'days', count: Math.ceil(absolute / 1440) }
  if (absolute >= 60) return { overdue: minutes <= 0, unit: 'hours', count: Math.ceil(absolute / 60) }
  return { overdue: minutes <= 0, unit: 'minutes', count: Math.max(1, Math.ceil(absolute)) }
}

// 首页只负责把员工送回业务来源，不在这里复制审批动作。未知类型返回
// null，避免把用户送到凭猜测拼出的错误页面。
export function approvalSourceLink(ref: ApprovalBusinessRef): ApprovalSourceLink | null {
  if (ref.bizType === 'CONTRACT') {
    return { path: '/contracts', query: { id: ref.bizId } }
  }
  if (ref.bizType === 'PURCHASE_ORDER' || ref.bizType === 'PURCHASE_ORDER_CHANGE') {
    return { path: '/purchase-orders', query: { order: ref.bizId } }
  }
  return null
}
