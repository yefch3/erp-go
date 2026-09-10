// 「回复全部」的收件人怎么算。
//
// 规矩照 Gmail：收件人是回信地址（有 Reply-To 用它，否则是发信人）；原信的
// To 和 Cc 里其余每个人进抄送——去掉**这次用来回信的那个地址**，去掉收件人
// 本人，去重。
//
// **只去掉一个，不是去掉名下全部。** 原来去掉的是这个人名下所有信箱，理由
// 写的是「一封信同时发到了他两个箱，回复全部不该把另一个箱抄上」。听起来
// 合理，实际不对：那几个箱是不同的业务线，客户把询价同时发给 sales@ 和
// inquiry@ 是有意的，回复时把另一个箱丢掉，那条线上的同事就再也看不到这轮
// 往来了。绑的箱越多，丢得越多——四个箱的时候一封发给四个人的信只剩一个
// 抄送，正是这个规则被报上来的样子。
//
// self 是这次回信要用的那个地址，也就是「你在哪个箱里看信就从哪个箱回」的
// 那个箱（EmailsPage 的 currentAccount，写信框的发件人默认值同源）。这份
// 信息在前端手上，所以规则放这儿而不是服务端；服务端只负责把邮件头拆成人
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
  /** 这次回信用的那个地址。空表示不知道，那就谁都不去掉。 */
  self?: string
}

function norm(s: string | undefined): string {
  return (s ?? '').trim().toLowerCase()
}

export function replyAllRecipients(input: ReplyAllInput): { to: MailParty; cc: MailParty[] } {
  const primaryEmail = norm(input.replyTo) || norm(input.fromEmail)
  const to: MailParty = { name: primaryEmail === norm(input.fromEmail) ? input.fromName ?? '' : '', email: primaryEmail }
  const skip = new Set<string>([primaryEmail])
  if (norm(input.self)) skip.add(norm(input.self))
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
