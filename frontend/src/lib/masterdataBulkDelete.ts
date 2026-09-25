export interface MasterDeleteRow { id: string; code: string; name: string; createdAt: string; version: string; blockedReason: string }
// Local calendar days, converted to instants. Advancing the calendar date (not
// adding 24 hours) preserves inclusive days across daylight-saving transitions.
export function deletionWindow(dates: string[]) {
  if (dates.length !== 2) throw new Error('DATE_RANGE')
  const parse = (value: string) => {
    if (!/^\d{4}-\d{2}-\d{2}$/.test(value)) throw new Error('DATE_RANGE')
    const [y, m, d] = value.split('-').map(Number)
    const date = new Date(y, m - 1, d)
    if (date.getFullYear() !== y || date.getMonth() !== m - 1 || date.getDate() !== d) throw new Error('DATE_RANGE')
    return date
  }
  const start = parse(dates[0]), end = parse(dates[1])
  if (start > end) throw new Error('DATE_RANGE')
  end.setDate(end.getDate() + 1)
  return { startAt: start.toISOString(), endAt: end.toISOString() }
}
export function deleteSelections(rows: MasterDeleteRow[], selected: string[]) {
  const wanted = new Set(selected)
  return rows.filter(row => wanted.has(row.id) && !row.blockedReason).map(row => ({ id: row.id, version: row.version }))
}
