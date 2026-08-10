// Country codes, and the browser's own translations of them.
//
// The list here is codes only. Names are presentation, this application is
// read in three languages, and every browser already ships the full set of
// translations for all of them — `Intl.DisplayNames` turns 'BR' into 巴西,
// Brasil or Brazil depending on who is looking. Shipping our own table would
// mean maintaining 249 × 3 strings that are already on the user's machine, and
// getting them subtly wrong in the two languages nobody on the team reads.

/** ISO 3166-1 alpha-2, every officially assigned code. */
export const COUNTRY_CODES: readonly string[] = [
  'AD', 'AE', 'AF', 'AG', 'AI', 'AL', 'AM', 'AO', 'AQ', 'AR', 'AS', 'AT', 'AU', 'AW', 'AX', 'AZ',
  'BA', 'BB', 'BD', 'BE', 'BF', 'BG', 'BH', 'BI', 'BJ', 'BL', 'BM', 'BN', 'BO', 'BQ', 'BR', 'BS',
  'BT', 'BV', 'BW', 'BY', 'BZ', 'CA', 'CC', 'CD', 'CF', 'CG', 'CH', 'CI', 'CK', 'CL', 'CM', 'CN',
  'CO', 'CR', 'CU', 'CV', 'CW', 'CX', 'CY', 'CZ', 'DE', 'DJ', 'DK', 'DM', 'DO', 'DZ', 'EC', 'EE',
  'EG', 'EH', 'ER', 'ES', 'ET', 'FI', 'FJ', 'FK', 'FM', 'FO', 'FR', 'GA', 'GB', 'GD', 'GE', 'GF',
  'GG', 'GH', 'GI', 'GL', 'GM', 'GN', 'GP', 'GQ', 'GR', 'GS', 'GT', 'GU', 'GW', 'GY', 'HK', 'HM',
  'HN', 'HR', 'HT', 'HU', 'ID', 'IE', 'IL', 'IM', 'IN', 'IO', 'IQ', 'IR', 'IS', 'IT', 'JE', 'JM',
  'JO', 'JP', 'KE', 'KG', 'KH', 'KI', 'KM', 'KN', 'KP', 'KR', 'KW', 'KY', 'KZ', 'LA', 'LB', 'LC',
  'LI', 'LK', 'LR', 'LS', 'LT', 'LU', 'LV', 'LY', 'MA', 'MC', 'MD', 'ME', 'MF', 'MG', 'MH', 'MK',
  'ML', 'MM', 'MN', 'MO', 'MP', 'MQ', 'MR', 'MS', 'MT', 'MU', 'MV', 'MW', 'MX', 'MY', 'MZ', 'NA',
  'NC', 'NE', 'NF', 'NG', 'NI', 'NL', 'NO', 'NP', 'NR', 'NU', 'NZ', 'OM', 'PA', 'PE', 'PF', 'PG',
  'PH', 'PK', 'PL', 'PM', 'PN', 'PR', 'PS', 'PT', 'PW', 'PY', 'QA', 'RE', 'RO', 'RS', 'RU', 'RW',
  'SA', 'SB', 'SC', 'SD', 'SE', 'SG', 'SH', 'SI', 'SJ', 'SK', 'SL', 'SM', 'SN', 'SO', 'SR', 'SS',
  'ST', 'SV', 'SX', 'SY', 'SZ', 'TC', 'TD', 'TF', 'TG', 'TH', 'TJ', 'TK', 'TL', 'TM', 'TN', 'TO',
  'TR', 'TT', 'TV', 'TW', 'TZ', 'UA', 'UG', 'UM', 'US', 'UY', 'UZ', 'VA', 'VC', 'VE', 'VG', 'VI',
  'VN', 'VU', 'WF', 'WS', 'YE', 'YT', 'ZA', 'ZM', 'ZW',
]

// One instance per language rather than one per call. Constructing an
// Intl formatter is not free, and this is called once per row of a list.
const displayCache = new Map<string, Intl.DisplayNames | null>()

function displayFor(locale: string): Intl.DisplayNames | null {
  if (!displayCache.has(locale)) {
    try {
      displayCache.set(locale, new Intl.DisplayNames([locale], { type: 'region' }))
    } catch {
      // A browser without region display names, or an unknown locale tag.
      // Falling back to the bare code is worse than a name and much better
      // than a blank cell.
      displayCache.set(locale, null)
    }
  }
  return displayCache.get(locale) ?? null
}

/**
 * countryName turns 'BR' into the reader's word for it.
 *
 * Returns the code itself when the browser has no name — an unassigned code,
 * or a runtime without Intl.DisplayNames. Never returns empty for a non-empty
 * code, because a row that shows nothing reads as missing data rather than as
 * a name we could not translate.
 */
export function countryName(code: string, locale: string): string {
  const c = (code || '').trim().toUpperCase()
  if (!c) return ''
  return displayFor(locale)?.of(c) ?? c
}

export interface CountryOption {
  code: string
  name: string
}

/**
 * countryOptions is the whole list, sorted the way the reader's language sorts
 * it. localeCompare rather than a plain sort, because 巴西 and 白俄罗斯 order
 * by pronunciation in Chinese and by code point in nothing anybody wants.
 */
export function countryOptions(locale: string): CountryOption[] {
  return COUNTRY_CODES.map((code) => ({ code, name: countryName(code, locale) })).sort((a, b) =>
    a.name.localeCompare(b.name, locale),
  )
}
