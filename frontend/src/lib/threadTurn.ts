// 一条会话里，每一行该说「谁写的」和「发给谁」。
//
// 这个文件存在的原因是一个被报上来的问题：界面上每行显示的是 counterparty，
// 而它在两个方向上**不是同一件事**——我发出的那行它是收件人，收到的那行它是
// 发件人。两行还都可能标着「我发出」（发信地址是自己名下的箱时就会这样），
// 于是同一条会话里上下两行的地址，一个是「发给谁」一个是「谁发的」，看的人
// 无从分辨。
//
// 后端现在两腿都填 fromEmail / toAll，含义一致。下面两个函数只做一件事：
// 优先用含义一致的那两个字段，拿不到时才按方向去解释 counterparty。
//
// 之所以还留着退路：前端会先于后端发上去，中间那一会儿 fromEmail 是空的，
// 而那时候显示一个按方向解释的地址，仍然比显示空白强。

export interface TurnLike {
  direction: string
  counterparty: string
  who?: string
  fromEmail?: string
  fromName?: string
  toAll?: string
}

/** 写这封信的那个地址。 */
export function turnSenderEmail(it: TurnLike): string {
  if (it.fromEmail) return it.fromEmail
  // 老后端：收到的那腿 counterparty 就是发件人；发出的那腿它是收件人，
  // 给不出发件人——宁可空着，也不要把收件人说成发件人。
  return it.direction === 'IN' ? it.counterparty || '' : ''
}

/** 显示成名字的那一段：有名字用名字，没有就用地址。 */
export function turnSenderLabel(it: TurnLike): string {
  return it.fromName || turnSenderEmail(it) || it.who || ''
}

/** 这封信发给了谁。整段，不是第一个。 */
export function turnRecipients(it: TurnLike): string {
  if (it.toAll) return it.toAll
  // 老后端：发出的那腿 counterparty 是收件人；收到的那腿给不出。
  return it.direction === 'OUT' ? it.counterparty || '' : ''
}
