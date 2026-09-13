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
  loadingPortId: string
  loadingPortCode: string
  loadingPortTimezone: string
  dischargePortId: string
  dischargePortCode: string
  dischargePortTimezone: string
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
  bookingNo: string
  billOfLadingNo: string
  warehouseEntryDate: string
  customsDeclarationDate: string
  freightCurrency: string
  freightAmount: string
}

export function shippingStatusLabel(schedule: Pick<ShippingSchedule, 'status'|'bookingNo'>, translate: (key:string)=>string): string {
  if (schedule.status === 'PLANNED') return schedule.bookingNo ? '待开船' : '待订舱'
  if (schedule.status === 'SAILED') return '运输中'
  return translate(`shipping.statuses.${schedule.status}`)
}

export interface ShippingRouteNode {
  id: string
  portId: string
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

export interface ShippingReminderPreference {
  leadDays: number[]
  timezone: string
  holidayCountryCodes: string[]
  calendarSyncStatus: 'NOT_CONFIGURED'|'SYNCED'|'STALE'|'FAILED'
  lastSyncAt: string
  lastSuccessAt: string
  lastError: string
}

export interface ShippingOperationalAlert {
  id: string
  scheduleId: string
  scheduleNo: string
  contractNo: string
  alertType: 'ETA_ADVANCED'|'ETD_DELAYED'|'WAREHOUSE_DELAYED'
  recipientEmployeeId: string
  recipientRole: string
  title: string
  content: string
  oldValue: string
  newValue: string
  dueDate: string
  readAt: string
  resolvedAt: string
  resolutionNote: string
  resolvedByName: string
  createdAt: string
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

export interface ShippingDocument {
  id: string
  scheduleId: string
  documentGroupKey: string
  category: string
  version: number
  fileName: string
  fileSize: string
  contentType: string
  remark: string
  status: 'ACTIVE' | 'VOIDED'
  uploadedBy: string
  uploadedByName: string
  uploadedAt: string
  voidedBy: string
  voidedByName: string
  voidedAt: string
  voidReason: string
}

export interface ShippingArrivalReminder {
  id: string
  scheduleId: string
  etaRevision: number
  targetEta: string
  dueAt: string
  status: string
  sentAt: string
  title: string
  content: string
  detailUrl: string
  readAt: string
  attemptCount: number
  lastError: string
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
