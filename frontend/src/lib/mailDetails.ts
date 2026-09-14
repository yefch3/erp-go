// 一封收到的信，「详情」里那几行。
//
// 抽出来是因为现在有两个地方要画它：邮件页的阅读区，和双击弹出的那个单独
// 窗口。两份各写一遍的话，哪天多一行「密送」就只会加在其中一处，而两处显示
// 的不是同一封信的详情，这种不一致没有人会当场发现。
//
// 纯函数，不认识 Vue：传进来一封信和一个翻译函数，回来一串「名 → 值」。

import { humanSize } from './humanSize'
import { zonedStamp } from './zonedtime'

export interface DetailMail {
  fromEmail?: string
  fromName?: string
  toEmail?: string
  toAll?: string
  cc?: string
  replyTo?: string
  subject?: string
  authSpf?: string
  authDkim?: string
  sentAt?: string
  receivedAt?: string
  folder?: string
  rawSize?: number
}

export type Translate = (key: string) => string

/**
 * 回信地址和发件人不是同一个。
 *
 * 这不是异常，正常业务里也常见（noreply 发出、客服组统一回收）。但它值得被
 * 看见：伪造一封看似来自老供应商的邮件、把 Reply-To 换成自己的地址，是骗走
 * 货款最常用的一手，而 From 那一行看上去毫无破绽。
 */
export function replyToDiffers(m: DetailMail | null | undefined): boolean {
  if (!m?.replyTo || !m.fromEmail) return false
  return m.replyTo.trim().toLowerCase() !== m.fromEmail.trim().toLowerCase()
}

export function mailDetailRows(m: DetailMail | null | undefined, t: Translate): { k: string; v: string }[] {
  if (!m) return []
  const rows: { k: string; v: string }[] = []
  const add = (k: string, v?: string | number) => {
    if (v !== undefined && v !== null && v !== '' && v !== 0) rows.push({ k, v: String(v) })
  }
  add(t('emails.detail.from'), `${m.fromName ? m.fromName + ' ' : ''}<${m.fromEmail}>`)
  // 整段，不是第一个：客户群发给七个人的信，这里要看到七个。
  add(t('emails.detail.to'), m.toAll || m.toEmail)
  add(t('emails.detail.cc'), m.cc)
  // 只在与 From 不同时才列：一样的时候它不是信息，是一行要读过去的字。
  if (replyToDiffers(m)) add(t('emails.detail.replyTo'), m.replyTo)
  add(t('emails.detail.subject'), m.subject)
  // Gmail 的 mailed-by / signed-by。空表示未记录或未通过验证 —— 两者都不该
  // 说成「验证失败」，那是在断言一件我们并不知道的事。
  add(t('emails.detail.mailedBy'), m.authSpf)
  add(t('emails.detail.signedBy'), m.authDkim)
  if (m.sentAt) add(t('emails.detail.sentAt'), zonedStamp(m.sentAt))
  if (m.receivedAt) add(t('emails.detail.receivedAt'), zonedStamp(m.receivedAt))
  add(t('emails.detail.folder'), m.folder)
  add(t('emails.detail.size'), m.rawSize ? humanSize(m.rawSize) : '')
  // Message-ID 不列。它只在追着邮件管理员查日志时有用，而摆在这儿的样子
  // 像一串谁都看不懂的乱码——用的人问过「这是不是出错了」。后端照样返回，
  // 要查的时候导出原件里有。
  return rows
}
