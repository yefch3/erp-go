// 把邮件拖进文件夹，两条规则。
//
// 都很短，但都有一个"想当然会写错"的地方，所以拿出来单独测：一条错了会
// 静静挪掉不该挪的信，另一条错了会让人对着一格拖半天放不下去。

/**
 * 这一次拖的到底是哪几封。
 *
 * **拖一行已经勾上的 = 拖整批；拖一行没勾的 = 只拖它。** 后半句还有一层：
 * 不改动勾选。Finder、Gmail、Outlook 都是这个规矩——拖一个没选中的东西
 * 不该把已选的清掉，否则人拖完一封回头发现刚才勾的十封没了。
 *
 * 拖不动的行（投递记录：邮箱服务器上没有正本，挪无处可挪）从批里剔掉，
 * 而不是让整次拖拽失败：勾了十封里混着一封投递记录，另外九封照挪。
 */
export function draggedRows<T extends { id: string }>(
  row: T,
  all: readonly T[],
  picked: readonly string[],
  movable: (r: T) => boolean,
): T[] {
  if (!movable(row)) return []
  if (!picked.includes(row.id)) return [row]
  return all.filter((r) => picked.includes(r.id) && movable(r))
}

/**
 * 这个信箱的文件夹接不接得住手上这几封。
 *
 * 跨信箱挪服务端会拒（folders.go：target.accountID != it.accountID 的那一
 * 条），所以这里先挡住——与其让人拖过去再看到失败提示，不如那一格不亮。
 *
 * **是「全都属于这个箱」，不是「有一封属于」。** 一次拖了两个箱的信
 * （搜索结果里做得到），放到任何一个箱的文件夹上都会挪一半失败；那种情况
 * 一格都不亮，比亮了再报「成功 3 失败 2」清楚。
 */
export function canDropInto(dragAccounts: readonly number[], accountId: number): boolean {
  return dragAccounts.length > 0 && dragAccounts.every((id) => id === accountId)
}
