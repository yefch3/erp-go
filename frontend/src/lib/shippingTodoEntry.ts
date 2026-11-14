const inquiryPath = '/shipping/requirements'
const orderPath = '/shipping/orders'

export function shippingHandoffStage(status: string) {
  if (status === 'WAITING_REQUOTE' || status === 'RETURNED') {
    return { path: inquiryPath, filter: status, actionKey: status === 'RETURNED' ? 'shipping.todoRetryInquiry' : 'shipping.d4Start' }
  }
  if (status === 'DRAFT' || status === 'PENDING_APPROVAL') {
    return { path: orderPath, filter: status, actionKey: 'shipping.todoViewOrder' }
  }
  if (['APPROVED', 'CONTRACT_UPLOADED', 'CONTRACT_VERIFIED', 'PAYMENT_REQUESTED', 'SCHEDULED'].includes(status)) {
    return { path: orderPath, filter: 'CONTRACT_AND_DELEGATED', actionKey: 'shipping.todoViewOrder' }
  }
  if (['CANCELLED', 'SUPERSEDED', 'CUSTOMER_MANAGED'].includes(status)) {
    return { path: orderPath, filter: status, actionKey: 'shipping.todoViewOrder' }
  }
  return null
}

export function shippingTodoEntry(row: { id: string; status: string }) {
  const stage = shippingHandoffStage(row.status)
  if (!stage || !/^[1-9]\d*$/.test(row.id)) return null
  return { to: { path: stage.path, query: { handoff: row.id } }, actionKey: stage.actionKey }
}
