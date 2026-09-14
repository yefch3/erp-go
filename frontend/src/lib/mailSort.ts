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

// 排序菜单里点了一项。
//
// 菜单里"按哪一列"和"哪个方向"是分开的两段（Foxmail 的排序菜单就是这样），
// 所以一条命令要么说列、要么说方向：`by:size`、`dir:asc`。
//
// 从前这里是 nextSort——"点同一列第二次翻方向"。那条规则配的是常驻在列表
// 上方的一排开关：开关就在眼前，点第二下看得见结果。搬进弹出菜单之后它不
// 成立了：点一下菜单就关上，人看不到自己把方向翻了，下次想翻还得再开一次
// 菜单猜一遍。方向改成菜单里两项明写的东西。
//
// 认不出的命令原样返回：菜单里只可能发出这两种，但命令是字符串，而字符串
// 总有一天会被别处拼错。那时该发生的事是"什么都没变"。
export function sortFromCommand(cur: MailSort, cmd: string): MailSort {
  const [kind, val] = cmd.split(':')
  if (kind === 'dir') {
    if (val !== 'asc' && val !== 'desc') return cur
    return cur.dir === val ? cur : { by: cur.by, dir: val }
  }
  if (kind !== 'by' || !val || !isField(val)) return cur
  // 点的就是当前这一列：方向归方向那两项管，这里什么都不做。
  if (cur.by === val) return cur
  return { by: val, dir: defaultDir(val) }
}

// 这一侧认不认这一列。切文件夹时排序会重置，所以对不上只可能是手写的
// 地址——那时按默认排，而不是把「按发件人」硬套到已发送上。
export function sortFor(side: 'inbox' | 'sent', s: MailSort): MailSort {
  return sortFieldsFor(side).includes(s.by) ? s : DEFAULT_SORT
}
