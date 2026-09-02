export interface MailCustomerDraft {
  name: string
  email: string
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
