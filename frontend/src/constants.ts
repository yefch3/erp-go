// Common trade-partner countries with their international calling codes.
// Names are spelled out in full — no abbreviations — and are what gets stored,
// so a record reads the same in every UI language. The selects still allow free
// entry, so a destination missing from this list is never blocked.
export interface Country {
  readonly name: string
  readonly dial: string
}

export const COUNTRIES: readonly Country[] = [
  { name: 'China', dial: '+86' },
  { name: 'United States', dial: '+1' },
  { name: 'Canada', dial: '+1' },
  { name: 'Germany', dial: '+49' },
  { name: 'United Kingdom', dial: '+44' },
  { name: 'France', dial: '+33' },
  { name: 'Italy', dial: '+39' },
  { name: 'Spain', dial: '+34' },
  { name: 'Portugal', dial: '+351' },
  { name: 'Netherlands', dial: '+31' },
  { name: 'Belgium', dial: '+32' },
  { name: 'Switzerland', dial: '+41' },
  { name: 'Sweden', dial: '+46' },
  { name: 'Poland', dial: '+48' },
  { name: 'Russia', dial: '+7' },
  { name: 'Turkey', dial: '+90' },
  { name: 'Japan', dial: '+81' },
  { name: 'South Korea', dial: '+82' },
  { name: 'India', dial: '+91' },
  { name: 'Vietnam', dial: '+84' },
  { name: 'Thailand', dial: '+66' },
  { name: 'Indonesia', dial: '+62' },
  { name: 'Malaysia', dial: '+60' },
  { name: 'Singapore', dial: '+65' },
  { name: 'Philippines', dial: '+63' },
  { name: 'Australia', dial: '+61' },
  { name: 'New Zealand', dial: '+64' },
  { name: 'Mexico', dial: '+52' },
  { name: 'Brazil', dial: '+55' },
  { name: 'Argentina', dial: '+54' },
  { name: 'Chile', dial: '+56' },
  { name: 'Colombia', dial: '+57' },
  { name: 'Peru', dial: '+51' },
  { name: 'United Arab Emirates', dial: '+971' },
  { name: 'Saudi Arabia', dial: '+966' },
  { name: 'Qatar', dial: '+974' },
  { name: 'Israel', dial: '+972' },
  { name: 'Egypt', dial: '+20' },
  { name: 'Morocco', dial: '+212' },
  { name: 'Nigeria', dial: '+234' },
  { name: 'Kenya', dial: '+254' },
  { name: 'South Africa', dial: '+27' },
]

export const COUNTRY_NAMES: readonly string[] = COUNTRIES.map((c) => c.name)

// One calling code can cover several countries (+1 is the United States and
// Canada), so the phone select groups them into a single option per code.
export interface DialCode {
  readonly dial: string
  readonly countries: string
}

export const DIAL_CODES: readonly DialCode[] = (() => {
  const byDial = new Map<string, readonly string[]>()
  for (const c of COUNTRIES) {
    byDial.set(c.dial, [...(byDial.get(c.dial) ?? []), c.name])
  }
  return [...byDial].map(([dial, names]) => ({ dial, countries: names.join(' / ') }))
})()

export function dialCodeOf(country: string): string {
  return COUNTRIES.find((c) => c.name === country)?.dial ?? ''
}

// Phones are stored as one string ("+49 211 5566 7788"). Editing needs the
// two halves back: the longest matching code wins so +972 is not read as +97.
export function splitPhone(phone: string): { dial: string; number: string } {
  const match = [...DIAL_CODES]
    .map((d) => d.dial)
    .sort((a, b) => b.length - a.length)
    .find((dial) => phone.startsWith(dial))
  if (!match) return { dial: '', number: phone }
  return { dial: match, number: phone.slice(match.length).trim() }
}
