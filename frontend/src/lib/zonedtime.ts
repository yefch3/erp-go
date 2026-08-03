// Wall-clock times in somebody else's zone.
//
// Scheduling a mail for "9am Monday" is never about our own clock: the hour
// that matters is the customer's. A local time plus a zone is not a moment
// until those two are resolved together, and the resolution has to happen
// here — the server stores an instant, and an instant cannot be shifted later
// by a daylight-saving change nobody remembered.

/** What a zone's UTC offset is at a given instant, in milliseconds. */
function offsetAt(at: Date, zone: string): number {
  const parts = new Intl.DateTimeFormat('en-US', {
    timeZone: zone,
    hour12: false,
    year: 'numeric',
    month: '2-digit',
    day: '2-digit',
    hour: '2-digit',
    minute: '2-digit',
    second: '2-digit',
  }).formatToParts(at)
  const n = (type: string) => Number(parts.find((p) => p.type === type)?.value ?? 0)
  // Reading the zone's wall clock as if it were UTC gives the offset back as
  // the difference from the real instant.
  const asUTC = Date.UTC(n('year'), n('month') - 1, n('day'), n('hour') % 24, n('minute'), n('second'))
  return asUTC - at.getTime()
}

/**
 * The instant at which a zone's clock reads the given wall time.
 *
 * Corrected twice on purpose. The first guess uses the offset in force at the
 * naive instant, which is the wrong side of a daylight-saving boundary for the
 * two nights a year when the clocks move; re-reading the offset at the guessed
 * instant lands on the right one.
 */
export function zonedToInstant(wall: string, zone: string): Date | null {
  const m = /^(\d{4})-(\d{2})-(\d{2})[ T](\d{2}):(\d{2})/.exec(wall)
  if (!m) return null
  const [, y, mo, d, h, mi] = m
  const naive = Date.UTC(+y, +mo - 1, +d, +h, +mi)
  let guess = naive - offsetAt(new Date(naive), zone)
  guess = naive - offsetAt(new Date(guess), zone)
  return new Date(guess)
}

/** A zone's wall clock at an instant, as `YYYY-MM-DD HH:mm`. */
export function wallClockIn(at: Date, zone: string): string {
  const parts = new Intl.DateTimeFormat('en-CA', {
    timeZone: zone,
    hour12: false,
    year: 'numeric',
    month: '2-digit',
    day: '2-digit',
    hour: '2-digit',
    minute: '2-digit',
  }).formatToParts(at)
  const v = (type: string) => parts.find((p) => p.type === type)?.value ?? '00'
  // Intl gives 24 for midnight in some engines; the date is already the right
  // day, so only the hour needs bringing back into range.
  const hour = v('hour') === '24' ? '00' : v('hour')
  return `${v('year')}-${v('month')}-${v('day')} ${hour}:${v('minute')}`
}

/** Shifts a `YYYY-MM-DD HH:mm` wall time by whole days, and sets the clock. */
export function wallAt(wall: string, addDays: number, hour: number, minute = 0): string {
  const m = /^(\d{4})-(\d{2})-(\d{2})/.exec(wall)
  if (!m) return wall
  const [, y, mo, d] = m
  const shifted = new Date(Date.UTC(+y, +mo - 1, +d + addDays))
  const pad = (n: number) => String(n).padStart(2, '0')
  return (
    `${shifted.getUTCFullYear()}-${pad(shifted.getUTCMonth() + 1)}-${pad(shifted.getUTCDate())}` +
    ` ${pad(hour)}:${pad(minute)}`
  )
}

/** Days from a wall date to the next given weekday (1 = Monday), never 0. */
export function daysToWeekday(wall: string, weekday: number): number {
  const m = /^(\d{4})-(\d{2})-(\d{2})/.exec(wall)
  if (!m) return 1
  const [, y, mo, d] = m
  const dow = new Date(Date.UTC(+y, +mo - 1, +d)).getUTCDay() || 7
  const ahead = (weekday - dow + 7) % 7
  return ahead === 0 ? 7 : ahead
}

/** The zone the browser is in. */
export function localZone(): string {
  return Intl.DateTimeFormat().resolvedOptions().timeZone || 'UTC'
}

/** `UTC+8`, `UTC-4:30` — how a zone reads right now, for a picker label. */
export function offsetLabel(zone: string, at = new Date()): string {
  const mins = Math.round(offsetAt(at, zone) / 60000)
  const sign = mins < 0 ? '-' : '+'
  const abs = Math.abs(mins)
  const h = Math.floor(abs / 60)
  const m = abs % 60
  return `UTC${sign}${h}${m ? ':' + String(m).padStart(2, '0') : ''}`
}
