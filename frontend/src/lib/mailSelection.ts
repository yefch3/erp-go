// 邮件文件夹里「勾选了哪些行」。
//
// 抽出来是因为它已经错过一次。工具条上的「已选 N 封」和全选框都是拿 inbound
// 算的——收件箱那批行——而已发送用的是另一份数据（mailboxSent）。于是在已发送
// 里勾一行：行会变色（MailList 认自己的 selected），工具条一个字都不出，
// 一个按钮都没有。点了没反应，也不报错。
//
// 根因是「屏幕上这一批行」这件事被写死成了某一个变量。这里把它变成一个要传
// 文件夹进来的函数，于是再多一个文件夹时，漏了就是编译不过或者测试挂，而不是
// 又一个勾了没反应的复选框。

/** 列表里能勾的一行。id 之外只关心它能不能被操作。 */
export interface PickableMail {
  id: string
  /**
   * 已发送独有。'ERP' 表示这封是我们自己的投递记录——邮件服务器的已发送里
   * 没有对应的信，所以它没有正本可以标记、归档或删除。
   */
  kind?: string
}

/** 各文件夹当前加载到的行。 */
export interface MailPools<T extends PickableMail> {
  /** 收件箱 / 星标 / 归档 / 垃圾邮件 / 废纸篓 / 搜索结果，共用这一份。 */
  inbound: T[]
  /** 已发送。 */
  sent: T[]
}

/** 用邮件列表（而不是表格）展示、并且能批量操作的文件夹。 */
const LIST_FOLDERS = new Set(['inbox', 'starred', 'archive', 'junk', 'trash', 'sent'])

export function isListFolder(folder: string): boolean {
  return LIST_FOLDERS.has(folder)
}

/**
 * 投递记录不给勾。
 *
 * MailList 早就不给它星标、不给它行内按钮了，理由是「宁可诚实地留个空，也不要
 * 一按就失败的按钮」。复选框当时漏了：全选之后它会是唯一没被勾上的一行，看起来
 * 像页面坏了；真发起批量操作，它那条请求也只会 404。
 */
export function isActionable(m: PickableMail): boolean {
  return m.kind !== 'ERP'
}

/**
 * 当前文件夹里能勾的那批行。
 *
 * 全选、「已选 N 封」、批量操作，三者必须问同一个函数——它们各自去猜「现在屏幕上
 * 是哪批」正是原来那个 bug 的来源。
 */
export function selectableRows<T extends PickableMail>(
  folder: string,
  pools: MailPools<T>,
): T[] {
  if (folder === 'sent') return pools.sent.filter(isActionable)
  if (isListFolder(folder)) return pools.inbound.filter(isActionable)
  return []
}

/** 勾中的行。只算还在屏幕上的：翻页或换文件夹之后，旧的勾选不该还能操作。 */
export function pickedRows<T extends PickableMail>(rows: T[], picked: string[]): T[] {
  const want = new Set(picked)
  return rows.filter((m) => want.has(m.id))
}

/**
 * 全选框的两个状态。
 *
 * some（半选）是给「点一下是清空还是补齐」这个问题的答案：勾了一部分的时候，
 * 框子显示成横杠，点下去是清空。
 */
export function selectAllState<T extends PickableMail>(
  rows: T[],
  picked: string[],
): { all: boolean; some: boolean } {
  const n = pickedRows(rows, picked).length
  return { all: rows.length > 0 && n === rows.length, some: n > 0 && n < rows.length }
}

/** 点全选框之后的新勾选。勾了一部分时，点下去是清空——不是「连剩下的一起勾上」。 */
export function toggleAll<T extends PickableMail>(rows: T[], picked: string[]): string[] {
  const { all, some } = selectAllState(rows, picked)
  return all || some ? [] : rows.map((m) => m.id)
}
