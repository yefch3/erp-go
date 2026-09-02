// 左栏那些文件夹，哪些跟着信箱走、哪些不跟。
//
// 这个分法不是排版偏好，是**后端的事实**：
//
//   - 收件箱 / 星标 / 已定时 / 已发送 / 归档 / 垃圾邮件 / 回收站 / 草稿箱
//     的查询都带 account_id，切到哪个箱看到的就是哪个箱的。
//   - 待处理（ListMessages）按人取，不按箱；拒收名单是整家公司一份。
//
// 所以后两个**不能挂在某个信箱底下**。挂了的话，三个信箱底下会各有一个
// 「待处理」，点开是同一份列表却各自声称是那个箱的——这比没有这一栏更坏，
// 和「一个会给出错误答案的表格比没有这张表格更糟」是同一条。

/** 跟着信箱走的，按左栏从上到下的顺序。 */
export const MAILBOX_FOLDER_KEYS = [
  'inbox',
  'starred',
  'drafts',
  'scheduled',
  'sent',
  'archive',
  'junk',
  'trash',
] as const

/** 不跟信箱走的，单独一栏。 */
export const SHARED_FOLDER_KEYS = ['attention', 'suppressions'] as const

export function isMailboxFolder(key: string): boolean {
  return (MAILBOX_FOLDER_KEYS as readonly string[]).includes(key)
}

export interface FolderDef {
  key: string
  icon: unknown
}

/** 把一份文件夹清单按上面那条事实拆成两半，顺序照清单里的来。 */
export function splitFolders(all: readonly FolderDef[]): {
  perMailbox: FolderDef[]
  shared: FolderDef[]
} {
  const byKey = new Map(all.map((f) => [f.key, f]))
  const pick = (keys: readonly string[]) =>
    keys.map((k) => byKey.get(k)).filter((f): f is FolderDef => !!f)
  return {
    perMailbox: pick(MAILBOX_FOLDER_KEYS),
    shared: pick(SHARED_FOLDER_KEYS),
  }
}

// ------------------------------------------------------------ 展开与收起

/**
 * 切到某个信箱之后，哪几个是展开的。
 *
 * 规矩只有两条：**切过去的那个一定展开**（你要读它，不能让人再点一下才
 * 看得见文件夹），其余保持人上次留下的样子。第一次进来时 saved 是空的，
 * 于是只有当前这个是开的——三个箱各挂八个文件夹全摊开，左栏会长到要滚动。
 */
export function expandedAfterSwitch(saved: readonly number[], current: number): number[] {
  const out = saved.filter((id) => id > 0)
  if (current > 0 && !out.includes(current)) out.push(current)
  return out
}

/** 点了三角：开的收起来，收的展开。 */
export function toggleExpanded(saved: readonly number[], id: number): number[] {
  return saved.includes(id) ? saved.filter((x) => x !== id) : [...saved, id]
}

/** localStorage 里存的那串。坏了就当没存过——一个手改坏的值不该让左栏白屏。 */
export function parseExpanded(raw: string | null): number[] {
  if (!raw) return []
  try {
    const v = JSON.parse(raw)
    if (!Array.isArray(v)) return []
    return v.map(Number).filter((n) => Number.isInteger(n) && n > 0)
  } catch {
    return []
  }
}
