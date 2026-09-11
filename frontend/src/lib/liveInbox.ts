// 新信到了，第二栏那份列表要不要跟着重拉。
//
// 收信那边（IMAP 同步）存完新信会经 SSE 推一条 `mail.inbound` 过来，页面听到
// 之后有两件事可做：刷左栏的角标，和重拉眼前的列表。角标永远刷；列表要不要
// 重拉，看的是这几条——它们决定的是「会不会把人正在做的事从手底下抽走」。
//
// **正在读一封信不再是理由。** 从前的写法是「开着一封信就不重拉」，那是给
// 两栏布局写的：那时读信会把列表整个盖住，重拉了也看不见。改成三栏之后列表
// 就在打开的信旁边，而一封信几乎总是开着的——于是那条规则让列表**永远**不
// 更新：角标上的数字在涨，列表纹丝不动，非得手动刷新。这就是 2026-09-12 报
// 的那个问题。
//
// 抽出来是因为它是一串判断，错一条的样子都是「有时候刷有时候不刷」，只有
// 逐条钉住才说得清。

import { isCustomFolderKey } from './mailFolders'

/** 页面此刻的样子，够这里做判断的那几样。 */
export interface ListState {
  /** 左栏站在哪：inbox / starred / … / 'F:自建'。 */
  folder: string
  /** 搜索状态：列表里是跨箱的命中，不是某个文件夹。 */
  searching: boolean
  /** 在第一页。翻到后面去了就别动——新信落在第一页，人正在看的是历史。 */
  onFirstPage: boolean
  /** 勾了几封。勾着的时候重拉会把勾选清掉（列表一换，勾选就作废）。 */
  picked: number
  /** 正拖着几封往文件夹上放。 */
  dragging: boolean
  /** 批量操作正在飞。 */
  bulkBusy: boolean
  /** 此刻看着哪个信箱。 */
  currentAccount: number
}

/**
 * 事件说的是哪个信箱。`MAILBOX:12` → 12。
 *
 * 认不出（老版本的服务端不带 subject，或者格式不对）回 0，调用方把 0 当成
 * 「不知道」——不知道就重拉，宁可多拉一次。
 */
export function mailboxOf(subject: string | undefined): number {
  const m = /^MAILBOX:(\d+)$/.exec(subject ?? '')
  return m ? Number(m[1]) : 0
}

/**
 * 新信可能落进哪些视图。收件箱和自建文件夹（服务器规则会直接分进去），还有
 * 垃圾邮件（服务器判定的）。星标、归档、废纸篓里不会凭空多出一封新信，
 * 已发送和草稿箱更不会。
 */
export function newMailLandsIn(folder: string): boolean {
  return folder === 'inbox' || folder === 'junk' || isCustomFolderKey(folder)
}

export function shouldReloadList(subject: string | undefined, s: ListState): boolean {
  if (!newMailLandsIn(s.folder)) return false
  if (s.searching) return false
  if (!s.onFirstPage) return false
  if (s.picked > 0 || s.dragging || s.bulkBusy) return false
  const box = mailboxOf(subject)
  // 说了是别的箱，就不动眼前这份；没说，当成可能是这个箱的。
  return box === 0 || box === s.currentAccount
}
