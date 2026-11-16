export interface DeliveryQualityTask {
  status: string
  procurementHandlingStatus?: string
  lines: Array<{
    requestedQty: string
    qualifiedQty: string
    finalResult: string
  }>
}

// This is an early UI explanation. The delivery service rechecks the current
// order, inspection evidence and quantities before accepting the operation.
export function hasPassedDeliveryInspection(tasks: readonly DeliveryQualityTask[]): boolean {
  return tasks.length > 0 && tasks.every(task =>
    task.status === 'COMPLETED' && task.procurementHandlingStatus !== 'PENDING' &&
    task.lines.length > 0 && task.lines.every(line => {
      const requested = Number(line.requestedQty)
      const qualified = Number(line.qualifiedQty)
      return line.finalResult === 'PASS' && Number.isFinite(requested) && requested > 0 &&
        Number.isFinite(qualified) && qualified >= requested
    }),
  )
}
