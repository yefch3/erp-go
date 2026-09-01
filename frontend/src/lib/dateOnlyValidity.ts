export function utcBusinessDate(now = new Date()): string {
  return now.toISOString().slice(0, 10)
}

export function isDateOnlyExpired(value?: string | null, now = new Date()): boolean {
  const date = String(value || '').trim().slice(0, 10)
  return Boolean(date && date < utcBusinessDate(now))
}
