import { recognizeMailContact } from './mailCustomerRecognition'
export interface MailCustomerDraft {
  name: string
  email: string
  companyName?: string
  phone?: string
  website?: string
  address?: string
}

export interface MailCustomerHeaders {
  fromName?: string
  fromEmail?: string
  toName?: string
  toEmail?: string
  bodyText?: string
  bodyHtml?: string
}

export interface MailCustomerDuplicateCandidate {
  id: string
  code: string
  name: string
  matchFields?: string[]
}

export interface MailCustomerPrecheck {
  emailOwner: MailCustomerDuplicateCandidate | null
  existingCompany: MailCustomerDuplicateCandidate | null
  suggestions: MailCustomerDuplicateCandidate[]
}

// The early check has three different meanings and the dialog must not blur
// them together: an exact email means "already done", one exact company means
// "add this person", and a similar name is only a suggestion for a human.
export function classifyMailCustomerDuplicates(
  candidates: MailCustomerDuplicateCandidate[],
): MailCustomerPrecheck {
  const emailOwner = candidates.find((candidate) =>
    candidate.matchFields?.includes('EMAIL'),
  ) ?? null
  if (emailOwner) {
    return {
      emailOwner,
      existingCompany: null,
      suggestions: candidates.filter((candidate) => candidate.id !== emailOwner.id),
    }
  }
  const existingCompany = existingCompanyForMailContact(candidates)
  return {
    emailOwner: null,
    existingCompany,
    suggestions: existingCompany
      ? candidates.filter((candidate) => candidate.id !== existingCompany.id)
      : candidates,
  }
}

// Only an unambiguous company identity may absorb a new mail contact. Similar
// names remain a human decision; automatically attaching there could put a
// correspondent under the wrong legal entity.
export function existingCompanyForMailContact<T extends MailCustomerDuplicateCandidate>(
  candidates: T[],
): T | null {
  const exact = candidates.filter((candidate) =>
    candidate.matchFields?.some((field) => field === 'NAME' || field === 'TAX_ID'),
  )
  return exact.length === 1 ? exact[0] : null
}

// 客户身份来自邮件头，而不是正文选区。正文里可能出现抄送人、签名、产品名
// 或历史引用；From 的姓名和邮箱才是这封来信明确声明的发件人。
export function customerDraftFromSender(
  fromName: string,
  fromEmail: string,
): MailCustomerDraft {
  const email = fromEmail.trim()
  const name = fromName.trim()
  return { name, email }
}

function normalized(address?: string): string {
  return address?.trim().toLowerCase() ?? ''
}

// A conversation row can reopen its newest copy. After we answer a customer,
// that newest copy is often the one in Sent: its From is us and the customer is
// in To. Looking only at From made "Create customer" disappear after the first
// reply even though the same external correspondent was still on screen.
export function customerDraftFromMail(
  mail: MailCustomerHeaders,
  ownAddresses: Iterable<string>,
): MailCustomerDraft | null {
  const own = new Set(Array.from(ownAddresses, normalized).filter(Boolean))
  const fromEmail = mail.fromEmail?.trim() ?? ''
  if (fromEmail && !own.has(normalized(fromEmail))) {
    let text = mail.bodyText ?? ''
    if (!text && mail.bodyHtml && typeof DOMParser !== 'undefined') {
      const doc = new DOMParser().parseFromString(mail.bodyHtml, 'text/html')
      doc.querySelectorAll('blockquote,script,style,.gmail_quote').forEach(node => node.remove())
      doc.querySelectorAll('br').forEach(node => node.replaceWith('\n'))
      doc.querySelectorAll('p,div,tr').forEach(node => node.append('\n'))
      text = doc.body.textContent ?? ''
    }
    const fields = recognizeMailContact(text)
    const sender = customerDraftFromSender(mail.fromName ?? '', fromEmail)
    return { ...fields, ...sender, name: sender.name || fields.name || '' }
  }

  const toEmail = mail.toEmail?.trim() ?? ''
  if (toEmail && !own.has(normalized(toEmail))) {
    return customerDraftFromSender(mail.toName ?? '', toEmail)
  }
  return null
}
