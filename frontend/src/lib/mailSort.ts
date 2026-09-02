// 邮件列表按哪一列排、往哪个方向排。
//
// 纯逻辑放这里而不是页面里，是为了能单独测：地址栏里的 `sort=size:desc`
// 怎么解、点同一列第二次是翻方向、切到已发送时「发件人」要换成「收件人」——
// 这些规则错了，屏幕上的样子都是「点了没反应」，只有测试能把它们钉住。
//
// 和服务端 ListSort 是同一套词：by ∈ from|to|subject|date|size，dir ∈ asc|desc。

export type SortField = 'from' | 'to' | 'subject' | 'date' | 'size'
export type SortDir = 'asc' | 'desc'

export interface MailSort {
  by: SortField
  dir: SortDir
}

// 一直以来的顺序：新的在前。地址栏里不写它，/emails 就是 /emails。
export const DEFAULT_SORT: MailSort = { by: 'date', dir: 'desc' }

const FIELDS: SortField[] = ['from', 'to', 'subject', 'date', 'size']

function isField(v: string): v is SortField {
  return (FIELDS as string[]).includes(v)
}

// 收件箱那一侧问「谁发来的」，已发送那一侧问「发给了谁」——同一个位置，
// 不同的问题，所以两份清单。
export function sortFieldsFor(side: 'inbox' | 'sent'): SortField[] {
  return side === 'sent' ? ['to', 'subject', 'date', 'size'] : ['from', 'subject', 'date', 'size']
}

// 每一列的自然方向：日期和大小是「新的/大的在前」，文字是 A 到 Z。
// 点一列第一次得到的就是它。
export function defaultDir(by: SortField): SortDir {
  return by === 'date' || by === 'size' ? 'desc' : 'asc'
}

// 地址栏里的样子：`size:desc`。默认排序是空串，免得每个链接都拖着一段
// 谁都没点过的参数。
export function sortParam(s: MailSort): string {
  if (s.by === DEFAULT_SORT.by && s.dir === DEFAULT_SORT.dir) return ''
  return `${s.by}:${s.dir}`
}

// 解地址栏。认不出的一律回默认：一个手改坏的链接该看到的是收件箱，不是白屏。
export function parseSort(raw: string | undefined | null): MailSort {
  if (!raw) return DEFAULT_SORT
  const [by, dir] = raw.split(':')
  if (!by || !isField(by)) return DEFAULT_SORT
  if (dir === 'asc' || dir === 'desc') return { by, dir }
  if (!dir) return { by, dir: defaultDir(by) }
  return DEFAULT_SORT
}

// 点了排序栏上的一列：点的是当前那列就翻方向，点的是别的列就按那列的
// 自然方向来。
export function nextSort(cur: MailSort, clicked: SortField): MailSort {
  if (cur.by === clicked) return { by: clicked, dir: cur.dir === 'asc' ? 'desc' : 'asc' }
  return { by: clicked, dir: defaultDir(clicked) }
}

// 这一侧认不认这一列。切文件夹时排序会重置，所以对不上只可能是手写的
// 地址——那时按默认排，而不是把「按发件人」硬套到已发送上。
export function sortFor(side: 'inbox' | 'sent', s: MailSort): MailSort {
  return sortFieldsFor(side).includes(s.by) ? s : DEFAULT_SORT
}
