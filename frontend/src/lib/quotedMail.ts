// 回复和转发时，原信被引用成什么样子。
//
// 抽出来是因为这段东西**会跟着信发到客户手上**，而它在写信框里只是一块看不见
// 边界的 HTML：改错了不会报错，只会在对方的邮箱里显示成一串标签，或者更糟，
// 把别人信里的一段文字当成标记执行掉。这里能在纯 node 里逐条断言。
//
// Issue #366：引用行原本只有「谁写道」，没有「什么时候」。一封询价来回四五轮
// 之后，收件人看到的是几层嵌套的引用块，每层都只署名不署时间，分不出哪一段
// 属于哪一轮。所有邮件客户端的引用行都带日期，正是这个原因。

import { zonedStamp } from './zonedtime'

/** 引用一封信需要知道的东西。 */
export interface QuotedSource {
  fromEmail: string
  fromName?: string
  /** 服务端净化过的 HTML 正文。 */
  bodyHtml?: string
  /** 没有 HTML 时的纯文本正文。**未转义**，由这里负责。 */
  bodyText?: string
  /** 原信是什么时候写的。发出的时刻优先，服务器落地的时刻兜底。 */
  sentAt?: string
  receivedAt?: string
}

/** vue-i18n 的 t，缩到这里用得到的那一点。 */
export type Translate = (key: string, params?: Record<string, unknown>) => string

const ESCAPES: Record<string, string> = {
  '&': '&amp;',
  '<': '&lt;',
  '>': '&gt;',
  '"': '&quot;',
  "'": '&#39;',
}

/**
 * 文字变成标记里的文字。
 *
 * 换行变 `<br>` 是有意的：纯文本正文里的分段只存在于换行符里，直接塞进 HTML
 * 会被折成一整段。转义在前、换行在后，所以别人信里写的 `<br>` 三个字还是三个字。
 */
export function escapeText(s: string): string {
  return s.replace(/[&<>"']/g, (c) => ESCAPES[c]).replace(/\n/g, '<br>')
}

/**
 * 引用行的原文（还没转义）。
 *
 * 整句话交给文案去拼，而不是在这里把日期、名字、地址一段段接起来：三种语言
 * 的语序不一样（中文「在…，…写道：」、英文 "On …, … wrote:"），接出来的
 * 句子只有中文是通顺的。
 *
 * 时间用 zonedStamp（`2026-03-01 16:00 +08:00`）而不是本地时间：这一行会跟着
 * 信发给客户，而客户多半不在这个时区，少了那个偏移量，「16:00」在对方眼里是
 * 一个不知道属于哪块表的数字。导出的会话记录用的也是这个格式。
 *
 * 没有时间戳时退回不带日期的那句：转发一封连时间都没记下的信是可能的，而
 * 「在 ，张三 写道：」比不说时间更糟。
 */
export function attributionLine(mail: QuotedSource, t: Translate): string {
  const who = mail.fromName || mail.fromEmail
  const when = mail.sentAt || mail.receivedAt || ''
  if (!when) return `${who} <${mail.fromEmail}> ${t('emails.wrote')}`
  return t('emails.quotedAt', { when: zonedStamp(when), who, addr: mail.fromEmail })
}

/**
 * 原信引用块：一行署名，底下一个 blockquote 装正文。所有邮件客户端都是这个
 * 形状，收件人的客户端也按这个形状去折叠嵌套的引用。
 *
 * 署名整行转义（地址上那对尖括号是文字，不是标签）；正文分两条路——HTML 那份
 * 是服务端净化过的，原样带走，因为它就是这封信本来的样子；纯文本那份在这里
 * 转义，它此前从来不是标记。
 */
export function quotedBlock(mail: QuotedSource, t: Translate): string {
  const inner = mail.bodyHtml || `<p>${escapeText(mail.bodyText || '')}</p>`
  return `<p>${escapeText(attributionLine(mail, t))}</p><blockquote>${inner}</blockquote>`
}
