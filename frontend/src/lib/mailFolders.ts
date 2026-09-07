// 左栏那些文件夹，哪些跟着信箱走、哪些不跟。
//
// 这个分法不是排版偏好，是**后端的事实**：
//
//   - 收件箱 / 星标 / 已定时 / 已发送 / 归档 / 垃圾邮件 / 回收站 / 草稿箱 /
//     待处理 的查询都带 account_id，切到哪个箱看到的就是哪个箱的。
//   - **拒收名单是整家公司一份**：`email_suppressions` 上是
//     `UNIQUE (tenant_id, email)`，发信时按公司拦。客户说过「别再发给我了」，
//     那句话对公司里每一个信箱都算数——挂到某个信箱底下，等于说换个地址发
//     就可以，而代码根本不是那么拦的。
//
// 所以拒收名单**不能挂在某个信箱底下**。挂了的话，三个信箱底下各有一个，
// 点开是同一份却各自声称是那个箱的——这比没有这一栏更坏，和「一个会给出
// 错误答案的表格比没有这张表格更糟」是同一条。

/** 跟着信箱走的，按左栏从上到下的顺序。 */
export const MAILBOX_FOLDER_KEYS = [
  'inbox',
  'starred',
  'drafts',
  'scheduled',
  'sent',
  'attention',
  'archive',
  'junk',
  'trash',
] as const

/** 不跟信箱走的，单独一栏。 */
export const SHARED_FOLDER_KEYS = ['suppressions'] as const

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

/**
 * 自建文件夹（Issue #362）。
 *
 * 它们的 key 就是后端的 view 参数：'F:' 加服务器上的名字。和上面那些固定
 * key 走同一条路（URL 的 folder、列表的 view），只是名字来自数据不来自文案。
 */
export const CUSTOM_FOLDER_PREFIX = 'F:'

export interface CustomFolder {
  id: number
  accountId: number
  name: string
  /** 后端给的 view 参数，形如 F:供应商。只有自建的（CUSTOM）有。 */
  viewKey: string
  /** INBOX / SENT / JUNK / TRASH / ARCHIVE / DRAFTS / SYSTEM / VIRTUAL / CUSTOM。 */
  role: string
}

/** 服务器上带角色的系统文件夹对应左栏哪个固定 key。 */
export const ROLE_FOLDER_KEY: Record<string, string> = {
  INBOX: 'inbox',
  SENT: 'sent',
  JUNK: 'junk',
  TRASH: 'trash',
  ARCHIVE: 'archive',
}

/** 左栏一个信箱下面的一行：固定视图或自建文件夹，同一级别。 */
export interface RailItem {
  key: string
  icon?: unknown
  /** 服务器上那个文件夹的名字（固定视图没有，用文案）。 */
  name?: string
  /** 用户自己建的：只有它有改名和删除。服务器自带的（SYSTEM）没有。 */
  custom: boolean
  /** 文件夹本身，改名/删除要用。 */
  folder?: CustomFolder
  /** 服务器上的实际名字，鼠标停上去看得到。 */
  hostName?: string
}

/**
 * 一个信箱下面按同一级别摆哪些行、什么顺序。
 *
 * 固定视图照 MAILBOX_FOLDER_KEYS 的顺序；「归档」只在服务器真有归档文件夹时
 * 出现（263、Gmail 有，163、126、QQ 没有）——服务器上没有就没有。
 *
 * 然后是**装着信的**那些服务器文件夹：先是服务器自带、ERP 不认得的
 * （163 的病毒文件夹、QQ 的其他文件夹），再是自建的，各自按名字排。两者都能
 * 点进去看信，只有自建的能改名删除。
 *
 * 不列的：服务器的草稿箱（下一期）、虚拟文件夹（Gmail 的标签，内容是别处的
 * 信的映射），以及已经有固定视图代表的收件箱/已发送/垃圾/回收站/归档。
 */
export function mailboxRail(fixed: readonly FolderDef[], hostFolders: readonly CustomFolder[]): RailItem[] {
  const byKey = new Map(fixed.map((f) => [f.key, f]))
  const hostByRole = new Map(hostFolders.map((f) => [f.role, f]))
  const out: RailItem[] = []
  for (const key of MAILBOX_FOLDER_KEYS) {
    const def = byKey.get(key)
    if (!def) continue
    if (key === 'archive' && !hostByRole.has('ARCHIVE')) continue
    const role = Object.entries(ROLE_FOLDER_KEY).find(([, k]) => k === key)?.[0]
    out.push({ key, icon: def.icon, custom: false, hostName: role ? hostByRole.get(role)?.name : undefined })
  }
  const byName = (a: CustomFolder, b: CustomFolder) => a.name.localeCompare(b.name, 'zh')
  const listed = (role: string) => hostFolders.filter((f) => f.role === role && f.viewKey).slice().sort(byName)
  for (const f of listed('SYSTEM')) {
    out.push({ key: f.viewKey, name: f.name, custom: false, folder: f, hostName: f.name })
  }
  for (const f of listed('CUSTOM')) {
    out.push({ key: f.viewKey, name: f.name, custom: true, folder: f, hostName: f.name })
  }
  return out
}

export function isCustomFolderKey(key: string): boolean {
  return key.startsWith(CUSTOM_FOLDER_PREFIX) && key.length > CUSTOM_FOLDER_PREFIX.length
}

/** 一个 folder key 该发给列表接口的 view。认不得的一律收件箱，和后端一致。 */
export function viewForFolderKey(key: string, fixed: Record<string, string>): string {
  if (key in fixed) return fixed[key]
  if (isCustomFolderKey(key)) return key
  return 'INBOX'
}

/** 和后端 validFolderName 同一套规矩，好让人在输入框里就看到原因。 */
export function folderNameProblem(name: string): string {
  const n = name.trim()
  if (!n) return 'empty'
  if ([...n].length > 120) return 'tooLong'
  if (/[/\\*%"]/.test(n) || /[\x00-\x1f\x7f]/.test(n)) return 'badChars'
  if (isProviderSystemFolder(n) || n.startsWith('[Gmail]')) return 'reserved'
  return ''
}

/**
 * 各家邮箱服务器自带的系统文件夹名，和后端 providerSystemFolders 同一份：
 * 只收各家真实的默认名，不收「像系统文件夹的词」——Gmail 上建个 Templates 是正当的。
 * 这些名字不能拿来建自建文件夹（263 会拒，网易会建出第二个同名的）。大小写不分。
 */
export const PROVIDER_SYSTEM_FOLDERS = [
  '收件箱', '草稿箱', '草稿夹', '已发送', '已发送邮件', '发件箱',
  '已删除', '已删除邮件', '垃圾邮件', '垃圾箱', '已归档',
  '病毒文件夹', '病毒邮件', '广告邮件', '订阅邮件',
  'INBOX', 'Drafts', 'Sent', 'Sent Messages', 'Sent Items',
  'Deleted Messages', 'Deleted Items', 'Trash', 'Junk', 'Spam', 'Archive',
]

export function isProviderSystemFolder(name: string): boolean {
  const n = name.trim().toLowerCase()
  return PROVIDER_SYSTEM_FOLDERS.some((s) => s.toLowerCase() === n)
}
