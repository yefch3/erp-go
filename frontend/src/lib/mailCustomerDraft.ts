export interface MailCustomerDraft {
  name: string
  email: string
}

export interface MailCustomerHeaders {
  fromName?: string
  fromEmail?: string
  toName?: string
  toEmail?: string
}

function fallbackName(email: string): string {
  const local = email.split('@')[0]?.trim() ?? ''
  return local || email
}

// 客户身份来自邮件头，而不是正文选区。正文里可能出现抄送人、签名、产品名
// 或历史引用；From 的姓名和邮箱才是这封来信明确声明的发件人。
export function customerDraftFromSender(
  fromName: string,
  fromEmail: string,
): MailCustomerDraft {
  const email = fromEmail.trim()
  const name = fromName.trim() || fallbackName(email)
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
    return customerDraftFromSender(mail.fromName ?? '', fromEmail)
  }

  const toEmail = mail.toEmail?.trim() ?? ''
  if (toEmail && !own.has(normalized(toEmail))) {
    return customerDraftFromSender(mail.toName ?? '', toEmail)
  }
  return null
}
