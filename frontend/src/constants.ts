// Common trade-partner countries with their international calling codes.
// Names are spelled out in full — no abbreviations — and are what gets stored,
// so a record reads the same in every UI language. The selects still allow free
// entry, so a destination missing from this list is never blocked.
export interface Country {
  // ISO 3166-1 alpha-2. Added when customers started being grouped by country
  // — the name here is now only the label on the calling-code select, which
  // groups several countries into one option and so needs words rather than
  // codes. Everywhere else, see lib/countries.ts: the code is stored and the
  // browser supplies the reader's own name for it.
  readonly code: string
  readonly name: string
  readonly dial: string
}

export const COUNTRIES: readonly Country[] = [
  { code: 'CN', name: 'China', dial: '+86' },
  { code: 'US', name: 'United States', dial: '+1' },
  { code: 'CA', name: 'Canada', dial: '+1' },
  { code: 'DE', name: 'Germany', dial: '+49' },
  { code: 'GB', name: 'United Kingdom', dial: '+44' },
  { code: 'FR', name: 'France', dial: '+33' },
  { code: 'IT', name: 'Italy', dial: '+39' },
  { code: 'ES', name: 'Spain', dial: '+34' },
  { code: 'PT', name: 'Portugal', dial: '+351' },
  { code: 'NL', name: 'Netherlands', dial: '+31' },
  { code: 'BE', name: 'Belgium', dial: '+32' },
  { code: 'CH', name: 'Switzerland', dial: '+41' },
  { code: 'SE', name: 'Sweden', dial: '+46' },
  { code: 'PL', name: 'Poland', dial: '+48' },
  { code: 'RU', name: 'Russia', dial: '+7' },
  { code: 'TR', name: 'Turkey', dial: '+90' },
  { code: 'JP', name: 'Japan', dial: '+81' },
  { code: 'KR', name: 'South Korea', dial: '+82' },
  { code: 'IN', name: 'India', dial: '+91' },
  { code: 'VN', name: 'Vietnam', dial: '+84' },
  { code: 'TH', name: 'Thailand', dial: '+66' },
  { code: 'ID', name: 'Indonesia', dial: '+62' },
  { code: 'MY', name: 'Malaysia', dial: '+60' },
  { code: 'SG', name: 'Singapore', dial: '+65' },
  { code: 'PH', name: 'Philippines', dial: '+63' },
  { code: 'AU', name: 'Australia', dial: '+61' },
  { code: 'NZ', name: 'New Zealand', dial: '+64' },
  { code: 'MX', name: 'Mexico', dial: '+52' },
  { code: 'BR', name: 'Brazil', dial: '+55' },
  { code: 'AR', name: 'Argentina', dial: '+54' },
  { code: 'CL', name: 'Chile', dial: '+56' },
  { code: 'CO', name: 'Colombia', dial: '+57' },
  { code: 'PE', name: 'Peru', dial: '+51' },
  { code: 'AE', name: 'United Arab Emirates', dial: '+971' },
  { code: 'SA', name: 'Saudi Arabia', dial: '+966' },
  { code: 'QA', name: 'Qatar', dial: '+974' },
  { code: 'IL', name: 'Israel', dial: '+972' },
  { code: 'EG', name: 'Egypt', dial: '+20' },
  { code: 'MA', name: 'Morocco', dial: '+212' },
  { code: 'NG', name: 'Nigeria', dial: '+234' },
  { code: 'KE', name: 'Kenya', dial: '+254' },
  { code: 'ZA', name: 'South Africa', dial: '+27' },
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

/**
 * dialCodeOfCode is dialCodeOf keyed on the ISO code, which is what customer
 * records now hold. Returns '' for the many countries not in the list above:
 * the calling code is a convenience, and offering none is better than offering
 * a wrong one.
 */
export function dialCodeOfCode(code: string): string {
  if (!code) return ''
  return COUNTRIES.find((c) => c.code === code)?.dial ?? ''
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
