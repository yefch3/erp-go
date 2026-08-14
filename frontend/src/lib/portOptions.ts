// 港口城市不是一个稳定的国际标准字典，因此这里只提供高频港口城市建议，
// 表单仍允许员工输入列表中没有的城市。UN/LOCODE 才是港口的唯一标准身份。
const COMMON_PORT_CITIES: Record<string, string[]> = {
  CN: ['上海', '天津', '宁波', '青岛', '深圳', '广州', '厦门', '大连', '连云港', '福州'],
  US: ['Los Angeles', 'Long Beach', 'New York', 'Newark', 'Seattle', 'Oakland', 'Houston', 'Savannah'],
  CA: ['Vancouver', 'Montreal', 'Halifax', 'Prince Rupert', 'Toronto'],
  MX: ['Manzanillo', 'Veracruz', 'Lázaro Cárdenas', 'Altamira'],
  BR: ['Santos', 'Rio de Janeiro', 'Paranaguá', 'Itajaí'],
  GB: ['Felixstowe', 'Southampton', 'London', 'Liverpool'],
  DE: ['Hamburg', 'Bremerhaven', 'Bremen'],
  NL: ['Rotterdam', 'Amsterdam'],
  BE: ['Antwerp', 'Zeebrugge'],
  FR: ['Le Havre', 'Marseille', 'Dunkirk'],
  ES: ['Valencia', 'Barcelona', 'Algeciras'],
  IT: ['Genoa', 'Trieste', 'La Spezia', 'Naples'],
  SG: ['Singapore'],
  MY: ['Port Klang', 'Tanjung Pelepas', 'Penang'],
  JP: ['Tokyo', 'Yokohama', 'Kobe', 'Osaka', 'Nagoya'],
  KR: ['Busan', 'Incheon', 'Gwangyang'],
  VN: ['Ho Chi Minh City', 'Hai Phong', 'Da Nang'],
  TH: ['Laem Chabang', 'Bangkok'],
  ID: ['Jakarta', 'Surabaya', 'Belawan'],
  IN: ['Mumbai', 'Chennai', 'Kolkata', 'Mundra', 'Nhava Sheva'],
  AE: ['Dubai', 'Abu Dhabi', 'Jebel Ali'],
  AU: ['Sydney', 'Melbourne', 'Brisbane', 'Fremantle'],
  NZ: ['Auckland', 'Tauranga', 'Wellington', 'Lyttelton'],
}

const COUNTRY_TIMEZONES: Record<string, string[]> = {
  CN: ['Asia/Shanghai'], HK: ['Asia/Hong_Kong'], TW: ['Asia/Taipei'], SG: ['Asia/Singapore'],
  JP: ['Asia/Tokyo'], KR: ['Asia/Seoul'], VN: ['Asia/Ho_Chi_Minh'], TH: ['Asia/Bangkok'],
  MY: ['Asia/Kuala_Lumpur'], ID: ['Asia/Jakarta', 'Asia/Makassar', 'Asia/Jayapura'],
  IN: ['Asia/Kolkata'], AE: ['Asia/Dubai'], GB: ['Europe/London'], DE: ['Europe/Berlin'],
  NL: ['Europe/Amsterdam'], BE: ['Europe/Brussels'], FR: ['Europe/Paris'], ES: ['Europe/Madrid'],
  IT: ['Europe/Rome'], US: ['America/New_York', 'America/Chicago', 'America/Denver', 'America/Los_Angeles', 'America/Anchorage', 'Pacific/Honolulu'],
  CA: ['America/St_Johns', 'America/Halifax', 'America/Toronto', 'America/Winnipeg', 'America/Edmonton', 'America/Vancouver'],
  MX: ['America/Mexico_City', 'America/Monterrey', 'America/Tijuana'], BR: ['America/Sao_Paulo', 'America/Manaus', 'America/Belem'],
  AU: ['Australia/Sydney', 'Australia/Melbourne', 'Australia/Brisbane', 'Australia/Adelaide', 'Australia/Perth'],
  NZ: ['Pacific/Auckland'],
}

export interface CommonPortOption {
  code: string
  nameZh: string
  nameEn: string
  city: string
  timezone: string
}

// 这里只放常用港口作为人工录入建议，不冒充完整的 UN/LOCODE 数据库。
// B3 后续接入审核后的标准数据文件后，这些选项将由后端港口库提供。
const COMMON_PORTS: CommonPortOption[] = [
  { code: 'CNSHA', nameZh: '上海港', nameEn: 'Shanghai', city: '上海', timezone: 'Asia/Shanghai' },
  { code: 'CNTNJ', nameZh: '天津港', nameEn: 'Tianjin', city: '天津', timezone: 'Asia/Shanghai' },
  { code: 'CNNGB', nameZh: '宁波港', nameEn: 'Ningbo', city: '宁波', timezone: 'Asia/Shanghai' },
  { code: 'CNQDG', nameZh: '青岛港', nameEn: 'Qingdao', city: '青岛', timezone: 'Asia/Shanghai' },
  { code: 'CNSZX', nameZh: '深圳港', nameEn: 'Shenzhen', city: '深圳', timezone: 'Asia/Shanghai' },
  { code: 'CNGZH', nameZh: '广州港', nameEn: 'Guangzhou', city: '广州', timezone: 'Asia/Shanghai' },
  { code: 'CNXMN', nameZh: '厦门港', nameEn: 'Xiamen', city: '厦门', timezone: 'Asia/Shanghai' },
  { code: 'CNDLC', nameZh: '大连港', nameEn: 'Dalian', city: '大连', timezone: 'Asia/Shanghai' },
  { code: 'USLAX', nameZh: '洛杉矶港', nameEn: 'Los Angeles', city: 'Los Angeles', timezone: 'America/Los_Angeles' },
  { code: 'USLGB', nameZh: '长滩港', nameEn: 'Long Beach', city: 'Long Beach', timezone: 'America/Los_Angeles' },
  { code: 'USNYC', nameZh: '纽约港', nameEn: 'New York', city: 'New York', timezone: 'America/New_York' },
  { code: 'CAVAN', nameZh: '温哥华港', nameEn: 'Vancouver', city: 'Vancouver', timezone: 'America/Vancouver' },
  { code: 'SGSIN', nameZh: '新加坡港', nameEn: 'Singapore', city: 'Singapore', timezone: 'Asia/Singapore' },
  { code: 'NLRTM', nameZh: '鹿特丹港', nameEn: 'Rotterdam', city: 'Rotterdam', timezone: 'Europe/Amsterdam' },
  { code: 'DEHAM', nameZh: '汉堡港', nameEn: 'Hamburg', city: 'Hamburg', timezone: 'Europe/Berlin' },
  { code: 'BEANR', nameZh: '安特卫普港', nameEn: 'Antwerp', city: 'Antwerp', timezone: 'Europe/Brussels' },
  { code: 'GBFXT', nameZh: '费利克斯托港', nameEn: 'Felixstowe', city: 'Felixstowe', timezone: 'Europe/London' },
  { code: 'JPYOK', nameZh: '横滨港', nameEn: 'Yokohama', city: 'Yokohama', timezone: 'Asia/Tokyo' },
  { code: 'KRPUS', nameZh: '釜山港', nameEn: 'Busan', city: 'Busan', timezone: 'Asia/Seoul' },
  { code: 'AEJEA', nameZh: '杰贝阿里港', nameEn: 'Jebel Ali', city: 'Dubai', timezone: 'Asia/Dubai' },
]

export function commonPortOptions(countryCode: string): CommonPortOption[] {
  return COMMON_PORTS.filter((port) => port.code.startsWith(countryCode))
}

export function portCityOptions(countryCode: string): string[] {
  return COMMON_PORT_CITIES[countryCode] ?? []
}

const allTimezones: string[] = (() => {
  const supported = (Intl as unknown as { supportedValuesOf?: (key: string) => string[] }).supportedValuesOf
  const zones = supported ? supported('timeZone') : []
  return Array.from(new Set(['UTC', ...zones])).sort()
})()

export function portTimezoneOptions(countryCode: string): string[] {
  const preferred = COUNTRY_TIMEZONES[countryCode] ?? []
  // 已知国家只展示该国时区，防止把中国港口误设成美国时区；未覆盖国家仍可从完整 IANA 列表选择。
  return preferred.length > 0 ? preferred : allTimezones
}

export function defaultPortTimezone(countryCode: string): string {
  return COUNTRY_TIMEZONES[countryCode]?.[0] ?? ''
}
