export interface ShippingSchedule {
  id: string
  scheduleNo: string
  contractId: string
  contractNo: string
  customerId: string
  customerName: string
  carrierId: string
  carrierForwarder: string
  vesselName: string
  voyageNo: string
  portOfLoading: string
  portOfDischarge: string
  etd: string
  atd: string
  eta: string
  originalEta: string
  ata: string
  responsibleEmployeeId: string
  responsibleName: string
  status: string
  remark: string
  createdBy: string
  createdByName: string
  createdAt: string
  updatedBy: string
  updatedByName: string
  updatedAt: string
  etaRevision: number
  routeVersion: number
  delayDays: number
  hasTemporaryCall: boolean
  currentProgress: string
}

export interface ShippingRouteNode {
  id: string
  sequenceNo: number
  nodeType: 'ORIGIN'|'TRANSIT'|'TEMPORARY'|'DESTINATION'
  portCode: string
  portName: string
  timezone: string
  originalEtaAt: string
  latestEtaAt: string
  actualArrivalAt: string
  originalEtdAt: string
  latestEtdAt: string
  actualDepartureAt: string
  nodeStatus: string
  remark: string
}

export interface ShippingDelayEvent {
  id: string
  impactType: string
  affectedNodeId: string
  fromNodeId: string
  toNodeId: string
  reasonCode: string
  reason: string
  note: string
  oldEta: string
  newEta: string
  changeDays: number
  cumulativeDelayDays: number
  operatorName: string
  createdAt: string
}

export interface ShippingStatistics {
  inTransit: string
  arrivingWithin7Days: string
  delayed: string
  temporaryCall: string
}

export interface ScheduleChange {
  id: string
  changeType: string
  fieldName: string
  oldValue: string
  newValue: string
  reason: string
  operatorId: string
  operatorName: string
  createdAt: string
}

export const SHIPPING_STATUSES = [
  'PLANNED', 'SAILED', 'IN_TRANSIT', 'ARRIVED', 'COMPLETED', 'DELAYED', 'CANCELLED',
] as const

export function statusTag(status: string) {
  if (status === 'COMPLETED') return 'success'
  if (status === 'CANCELLED') return 'info'
  if (status === 'DELAYED') return 'danger'
  if (status === 'ARRIVED') return 'success'
  if (status === 'SAILED' || status === 'IN_TRANSIT') return 'primary'
  return 'warning'
}
