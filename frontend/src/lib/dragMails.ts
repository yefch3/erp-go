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

/**
 * 放下之后要做的那件事。
 *
 * **不是每个格子都是"挪进某个文件夹"。** 界面上它们排在一起、看着是一排
 * 同类的东西，底下却是两套机制：
 *
 *   挪（move）—— 收件箱、垃圾邮件、自建文件夹。信在服务器上真的换了文件夹，
 *                ERP 这边 folder 跟着改。
 *   标（mark）—— 回收站、归档。视图看的是 deleted_at / archived_at 两个时间
 *                戳，不是 folder；改 folder 会让信从收件箱消失却不出现在
 *                回收站里，两边从此各说各的（见库里的 mail_view_of）。
 *
 * 回 null 表示这一格不接。星标是"标签不是位置"；已发送挪进去之后一封收到
 * 的信会显示成"我发出的"；草稿箱是另一张表，一封信变不成草稿；待处理和
 * 拒收名单根本不是邮件文件夹。
 */
export type DropTarget =
  | { kind: 'move'; folderId: number }
  | { kind: 'mark'; flags: Record<string, boolean> }

export function dropTargetFor(
  key: string,
  opts: { folderId?: number; junkFolderId?: number } = {},
): DropTarget | null {
  if (key.startsWith('F:')) {
    // 自建文件夹和服务器自带但 ERP 不认得的（病毒文件夹之类）都走这条。
    // 没有 id 就接不了——服务端要靠 id 找那个文件夹。
    return opts.folderId ? { kind: 'move', folderId: opts.folderId } : null
  }
  switch (key) {
    case 'inbox':
      // 0 是"收件箱"这个约定，服务端 resolveMoveTarget 认它。
      return { kind: 'move', folderId: 0 }
    case 'junk':
      // 真的挪进服务器的垃圾箱——服务商的过滤器靠这个学。左栏那一格是个
      // 固定视图，没带 id，所以 id 要从这个箱的文件夹清单里按角色找出来；
      // 找不到（服务器没有垃圾箱）就不接。
      return opts.junkFolderId ? { kind: 'move', folderId: opts.junkFolderId } : null
    case 'trash':
      return { kind: 'mark', flags: { deleted: true } }
    case 'archive':
      return { kind: 'mark', flags: { archived: true } }
    default:
      return null
  }
}
