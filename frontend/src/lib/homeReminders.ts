export type HomeReminderTiming = 'OVERDUE' | 'UPCOMING' | 'REMINDER'
export type HomeReminderSource = 'RECEIVABLE' | 'ARRIVAL' | 'BL' | 'SHIPPING_ACTION'

export interface HomeReminder {
  key: string
  source: HomeReminderSource
  sourceId: string
  title: string
  content: string
  bizNo: string
  dueAt: string
  createdAt: string
  unread: boolean
  timing: HomeReminderTiming
  detailUrl: string
}

export interface HomeReminderSummary {
  upcoming: number
  overdue: number
  unread: number
}

export interface HomeReminderSourceState {
  source: HomeReminderSource
  available: boolean
  message?: string
}

const reminderChangedEvent = 'erp:personal-reminders-changed'

export function homeReminderTagType(timing: HomeReminderTiming): 'danger' | 'warning' | 'info' {
  if (timing === 'OVERDUE') return 'danger'
  if (timing === 'UPCOMING') return 'warning'
  return 'info'
}

// 首页和顶部提醒组件共用这个轻量事件；真正状态仍以服务端提醒表为准。
export function announceReminderChanged() {
  if (typeof window !== 'undefined') window.dispatchEvent(new Event(reminderChangedEvent))
}

export function onReminderChanged(handler: () => void): () => void {
  if (typeof window === 'undefined') return () => undefined
  window.addEventListener(reminderChangedEvent, handler)
  return () => window.removeEventListener(reminderChangedEvent, handler)
}
