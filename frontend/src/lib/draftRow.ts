// 一封草稿在草稿箱列表里长什么样。
//
// 草稿箱从前是一张 el-table（主题 / 保存时间 / 一个删除按钮），和收件箱、
// 已发送完全两副样子。改成同一个三行式列表之后，左栏点来点去看到的是同一种
// 东西——而这正是「文件夹」这个说法本来的意思。
//
// 转换单独放在这里而不是写在页面里，因为它全是**没有答案时说什么**的判断：
// 草稿可以没有收件人、没有主题、没有正文，三样可以同时没有。一个空行是不能
// 点的：人要在列表里认出「我上周写了一半那封」，靠的恰恰是这三样里还剩下的
// 那一样。

import type { MailRow } from '../components/MailList.vue'

/** 列表接口给的一封草稿。字段是 Draft 的子集，只列这里用得到的。 */
export interface DraftSummary {
  id: string
  subject?: string
  snippet?: string
  updatedAt?: string
  recipients?: { email: string; name?: string }[]
  recipientCount?: number
  hasAttachments?: boolean
}

/** 没有的东西怎么说。由页面传进来，好让三种语言各说各的。 */
export interface DraftLabels {
  /** 一个收件人都没有。 */
  noRecipient: string
  /** 收件人多于一个：`{first} 等 {n} 人`。 */
  andMore: (first: string, n: number) => string
}

/**
 * 这封草稿是**写给谁**的，一行话。
 *
 * 草稿列表第一行回答的问题和收件箱相反：收到的信问「谁写的」，写了一半的信
 * 问「写给谁」。已发送早就是这个口径（MailList 里的 sentWho），草稿跟它一致。
 *
 * 有名字用名字，没名字用地址——名字是人认得出的那个，地址是一定有的那个。
 * 多个人时说「第一个 等 N 人」而不是把地址全列出来：这一列只有 280–400px，
 * 三个地址排下去就没有主题的位置了，而「等 3 人」已经答完了这个问题。
 */
export function draftWho(d: DraftSummary, labels: DraftLabels): string {
  const list = (d.recipients ?? []).filter((r) => r && (r.name || r.email))
  if (!list.length) return labels.noRecipient
  const first = list[0].name || list[0].email
  // recipientCount 是服务端数的，可能比 recipients 长（将来若截断）；取大的
  // 那个，宁可说多了也别说少了——「等 3 人」比「等 2 人」错得轻。
  const n = Math.max(list.length, Number(d.recipientCount ?? 0))
  return n > 1 ? labels.andMore(first, n) : first
}

/**
 * 草稿变成列表里的一行。
 *
 * 几个字段是有意钉死的：
 *
 *   · isRead 永远 true。草稿没有已读未读——它是自己写的。给成 false 会让整列
 *     加粗，而加粗在这份列表里的意思是「还没看过」。
 *   · isStarred 永远 false，MailList 那边草稿也不画星标：星标要写到邮件服务器
 *     上，而草稿只在我们自己的库里，没有正本可标。
 *   · receivedAt 用的是 updatedAt。列表那一列问的是「什么时候」，对一封收到
 *     的信是收到的时刻，对一封写了一半的信是**存下来的时刻**，同一个问题。
 */
export function draftToRow(d: DraftSummary, labels: DraftLabels): MailRow {
  const who = draftWho(d, labels)
  return {
    id: d.id,
    fromEmail: '',
    fromName: '',
    // 走 MailList 里「写给谁」那条路（和已发送同一条）。toAll 是整段收件人，
    // toEmail 留空——它是给「只有一个人」那种情况准备的精确地址，而这里的
    // toAll 已经是给人看的完整说法了。
    toAll: who,
    toName: who,
    toEmail: '',
    subject: d.subject ?? '',
    snippet: d.snippet ?? '',
    receivedAt: d.updatedAt ?? '',
    isRead: true,
    isStarred: false,
    hasAttachments: !!d.hasAttachments,
  }
}
