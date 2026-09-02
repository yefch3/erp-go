// 「回复全部」的收件人怎么算。
//
// 规矩照 Gmail：收件人是回信地址（有 Reply-To 用它，否则是发信人）；原信的
// To 和 Cc 里其余每个人进抄送——**去掉自己的地址**（自己给自己抄送是每个邮件
// 客户端都替人省掉的一步），去掉收件人本人，去重。
//
// mine 是这个人名下**全部**信箱的地址，不是当前这一个：一封信同时发到了他
// 两个箱，回复全部不该把另一个箱抄上。这份名单在前端手上（EmailsPage 的
// myAddresses），所以规则放这儿而不是服务端；服务端只负责把邮件头拆成人
// （parties.go）。

export interface MailParty {
  name?: string
  email: string
}

export interface ReplyAllInput {
  fromEmail: string
  fromName?: string
  replyTo?: string
  toParties?: MailParty[]
  ccParties?: MailParty[]
  /** 我名下全部信箱的地址。 */
  mine: Iterable<string>
}

function norm(s: string | undefined): string {
  return (s ?? '').trim().toLowerCase()
}

export function replyAllRecipients(input: ReplyAllInput): { to: MailParty; cc: MailParty[] } {
  const primaryEmail = norm(input.replyTo) || norm(input.fromEmail)
  const to: MailParty = { name: primaryEmail === norm(input.fromEmail) ? input.fromName ?? '' : '', email: primaryEmail }
  const skip = new Set<string>([primaryEmail])
  for (const m of input.mine) skip.add(norm(m))
  const cc: MailParty[] = []
  for (const p of [...(input.toParties ?? []), ...(input.ccParties ?? [])]) {
    const email = norm(p.email)
    if (!email) continue
    // 收件人本人可能就在原信的 To 里（Reply-To 指向一个也收了信的地址）：
    // 名字从这里补，但人不再抄一次。
    if (email === to.email && !to.name) to.name = p.name ?? ''
    if (skip.has(email)) continue
    skip.add(email)
    cc.push({ name: p.name ?? '', email })
  }
  return { to, cc }
}
