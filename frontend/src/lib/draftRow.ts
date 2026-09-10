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
/**
 * 右边那封草稿此刻挂在什么上下文里。**这个值变了，预览就得收起来。**
 *
 * 写成一个算出来的值，而不是在页面上列几条「什么时候清空」——列触发条件正是
 * 这段代码第一版犯的错，而漏掉的两条都不是想得到的：
 *
 *   一、**搜索不换文件夹。** 搜索框是整页唯一的那个（在左栏），成不成立只看
 *       关键词，不看站在哪个文件夹。于是在草稿箱里一搜：左边整列换成跨信箱
 *       的命中，folder 还是 'drafts'，右边那封草稿一动不动。
 *   二、**换信箱可能落在同一个 key 上。** 点另一个箱底下的「草稿箱」时，
 *       folder 被重新赋成同一个字符串，watch 不响——而列表已经是另一个箱的
 *       草稿了。取单封草稿的接口只按 tenant+owner+id 查、不按信箱查（只有
 *       列表按信箱），所以右边那封别的箱的草稿照样取得到，一句报错都没有。
 *
 * 搜索状态压成一个词而不是关键词本身：改关键词只是换一批命中，右边那封草稿
 * 早在开始搜的那一刻就该收起来了，没必要每敲一个字再清一次。
 */
export function draftPreviewContext(
  accountId: number,
  folder: string,
  searching: boolean,
): string {
  return `${accountId}/${searching ? '搜索' : folder}`
}

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
