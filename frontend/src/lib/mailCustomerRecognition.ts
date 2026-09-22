export interface RecognizedMailContact { companyName?: string; name?: string; phone?: string; website?: string; address?: string }

// Extract only explicit text from the current message. Quoted correspondence
// and arbitrary instructions in the mail are data, never actions to execute.
export function recognizeMailContact(body: string): RecognizedMailContact {
  const current = body.slice(0, 50000).split(/\r?\n/).filter(line => !/^\s*>/.test(line))
  const stop = current.findIndex(line => /^\s*(?:On .+wrote:|在.+写道[：:]|[-_]{2,}\s*(?:Original Message|原始邮件|Forwarded message)|From\s*:|发件人\s*[：:])/i.test(line))
  const lines = (stop >= 0 ? current.slice(0, stop) : current).map(line => line.trim()).filter(Boolean)
  const result: RecognizedMailContact = {}
  const explicit = (pattern: RegExp) => lines.map(line => pattern.exec(line)?.[1]?.trim()).find(Boolean)
  result.companyName = explicit(/^(?:Company(?: name)?|Organization|公司(?:名称)?|企业名称)\s*[:：]\s*(.{2,200})$/i)
  result.name = explicit(/^(?:Contact(?: name)?|Name|姓名|联系人)\s*[:：]\s*([^:：@]{1,100})$/i)
  result.phone = explicit(/^(?:Tel(?:ephone)?|Phone|Mobile|电话|手机)\s*[:：]\s*([+\d][\d\s()+.\-]{4,40})$/i)
  result.address = explicit(/^(?:Address|地址|公司地址)\s*[:：]\s*(.{3,500})$/i)
  result.website = explicit(/^(?:Website|Web|网址|网站)\s*[:：]\s*(https?:\/\/[^\s<>]+)$/i)
  const sign = lines.findIndex(line => /^(?:best regards|kind regards|regards|sincerely|此致|顺祝商祺)[,!，！。\s]*$/i.test(line))
  const signature = sign >= 0 ? lines.slice(sign + 1, sign + 14) : lines.slice(-10)
  if (!result.companyName) {
    const candidates = signature.filter(line => line.length <= 200 && !/[@:：!?。！]/.test(line) && /(?:\b(?:Ltd\.?|Limited|Inc\.?|LLC|GmbH|Corporation|Co\.,?\s*Ltd\.?)|有限公司|有限责任公司|集团公司)\s*$/i.test(line))
    if (new Set(candidates).size === 1) result.companyName = candidates[0]
  }
  if (!result.name && sign >= 0) {
    const candidate = signature[0] ?? ''
    if (candidate !== result.companyName && /^(?:[\p{L}][\p{L}.'’-]*\s+){1,3}[\p{L}][\p{L}.'’-]*$/u.test(candidate)) result.name = candidate
  }
  return Object.fromEntries(Object.entries(result).filter(([,value]) => Boolean(value)))
}

export interface MailContact { id: string; name: string; email: string; status: string; additionalEmails?: string[]; phone?: string }
export interface MailCompanyMatch { id: string; code: string; name: string; status: string; exactName: boolean; contacts?: MailContact[] }
export interface MailCustomerLink { inboundId?: string; customer?: {id: string; name: string; code: string; status?: string}; contact?: MailContact; email?: string }
export const normalizedMailValue = (v: string) => v.trim().toLowerCase()
export function mailContactMatches(c: MailContact, email: string): boolean {
  return c.status === 'ACTIVE' && [c.email, ...(c.additionalEmails ?? [])].some(v => normalizedMailValue(v) === normalizedMailValue(email))
}
export function resolveMailCustomer(companies: MailCompanyMatch[], email: string, name: string, selectedId = '') {
  const active = companies.filter(c => c.status === 'ACTIVE')
  const owners = active.flatMap(company => (company.contacts ?? []).filter(c => mailContactMatches(c,email)).map(contact => ({company,contact})))
  const exact = active.filter(c => c.exactName)
  const selected = active.find(c => c.id === selectedId) ?? null
  // A conflicting company name must be reviewed, even for an exact email.
  const soleOwner = owners.length === 1 ? owners[0] : null
  const conflict = Boolean(soleOwner && name.trim() && !soleOwner.company.exactName && !selected)
  const company = selected ?? (!conflict && soleOwner ? soleOwner.company : owners.length === 0 && exact.length === 1 ? exact[0] : null)
  const contacts = company?.contacts?.filter(c => c.status === 'ACTIVE') ?? []
  return { owners, company, contacts, conflict, ambiguous: !selected && (owners.length > 1 || (owners.length === 0 && exact.length > 1)) }
}
