// 组织架构图的两棵树。
//
// 「部门树」和「汇报线」在这个系统里是**两棵不同的树**，这是后端一开始就定下的
// （services/iam/db/migrations/00004_manager.sql 的注释：矩阵汇报、副职、
// 人在 A 部门却向 B 部门汇报）。所以这里也不去从一棵推另一棵。
//
// 抽出来单独写，是因为「画丢人」这件事在页面上看不出来——少一个方块，
// 谁都不会发现，除非那个人自己去找。下面每个边界都对应一种画丢的方式。

export interface OrgMember {
  id: string
  code: string
  name: string
  englishName: string
  position: string
  status: string
  /** 上级。'0' 或空表示头上没有人。 */
  managerId: string
  departmentId: string
  departmentName: string
  avatarUrl: string
  leaveDate: string
}

export interface OrgDepartment {
  id: string
  name: string
  parentId: string
  sortOrder: number
  status: string
  leaderEmployeeId: string
}

/** 树上的一个节点。人和部门共用一种形状，好让同一个组件画两棵树。 */
export interface OrgNode {
  /** el-tree 的 row-key。人和部门可能撞 id，所以带前缀。 */
  key: string
  kind: 'member' | 'department'
  label: string
  member?: OrgMember
  department?: OrgDepartment
  children: OrgNode[]
  /**
   * 这个人本该挂在别人下面，但那个上级不在名单里（多半已离职）。
   * 标出来而不是默默提到顶层——顶层多出一个人，看图的人只会以为他就该在那儿。
   */
  orphaned?: boolean
}

const NO_ONE = new Set(['', '0', undefined as unknown as string])

function hasManager(m: OrgMember): boolean {
  return !NO_ONE.has(m.managerId)
}

/**
 * 汇报线。**这是一片森林，不是一棵树**——头上没有人的不止一个（老板、
 * 还没配上级的新人、独立汇报的岗位）。
 *
 * 三种会画丢人的情况，都在这里挡住：
 *
 *  1. **上级不在名单里。** 名单只含在职员工，所以上级一离职，他的下属就成了
 *     指向空气的节点。朴素的建树写法会把这些人整个丢掉——而那恰恰是最需要
 *     被看见的一批人（没人管了）。这里提到顶层并打上 orphaned。
 *  2. **成环。** 写入时后端已经拦了任意深度的环（ManagerCycleExists），
 *     但读的这一侧不能假设数据一定干净——真环了，朴素写法是死循环，
 *     浏览器直接卡死。
 *  3. **上级是自己。** 环的退化情况，同上。
 */
export function buildReportingTree(members: OrgMember[]): OrgNode[] {
  const byID = new Map<string, OrgMember>()
  for (const m of members) byID.set(m.id, m)

  const nodes = new Map<string, OrgNode>()
  for (const m of members) {
    nodes.set(m.id, { key: `m${m.id}`, kind: 'member', label: m.name, member: m, children: [] })
  }

  const roots: OrgNode[] = []
  for (const m of members) {
    const node = nodes.get(m.id)!
    if (!hasManager(m) || m.managerId === m.id) {
      roots.push(node)
      continue
    }
    const parent = nodes.get(m.managerId)
    if (!parent) {
      // 上级不在名单里。提到顶层并标记——见上面第 1 条。
      node.orphaned = true
      roots.push(node)
      continue
    }
    // 成环检查：顺着上级往上走，如果又回到自己，就不挂了。
    if (leadsBackTo(m.id, m.managerId, byID)) {
      node.orphaned = true
      roots.push(node)
      continue
    }
    parent.children.push(node)
  }

  sortForest(roots)
  return roots
}

// leadsBackTo：从 startManager 往上走，会不会走回 employee 自己。
// 步数设上限，因为「走不回去但也走不完」（数据里有一段独立的环）同样会转不停。
function leadsBackTo(employeeID: string, startManager: string, byID: Map<string, OrgMember>): boolean {
  let cur = startManager
  for (let hops = 0; hops < 64; hops++) {
    if (cur === employeeID) return true
    const up = byID.get(cur)
    if (!up || !hasManager(up)) return false
    cur = up.managerId
  }
  // 走了 64 层还没到头，这条汇报线本身就有问题。当成环处理，
  // 宁可把这个人提到顶层，也不要画出一条转不完的线。
  return true
}

/**
 * 部门树，每个部门下面挂着这个部门的人。
 *
 * 两处和汇报线一样会画丢：
 *
 *  1. **父部门不存在**（被停用或删了）——子部门提到顶层，不丢。
 *  2. **人的部门不在部门表里**——单独归到一个「未分配部门」的节点。
 *     朴素写法会让这些人凭空消失，而部门数据出问题时，正是要看见谁受影响。
 *
 * 空部门**照样显示**：一个还没进人的部门是刚成立还是被掏空了，正是看图的人
 * 想知道的事。从员工反推部门会让它凭空消失。
 */
export function buildDepartmentTree(
  departments: OrgDepartment[],
  members: OrgMember[],
  unassignedLabel = '未分配部门',
): OrgNode[] {
  const active = departments.filter((d) => d.status === 'ACTIVE')
  const nodes = new Map<string, OrgNode>()
  for (const d of active) {
    nodes.set(d.id, {
      key: `d${d.id}`,
      kind: 'department',
      label: d.name,
      department: d,
      children: [],
    })
  }

  const roots: OrgNode[] = []
  for (const d of active) {
    const node = nodes.get(d.id)!
    const parent = NO_ONE.has(d.parentId) ? undefined : nodes.get(d.parentId)
    if (parent && parent !== node) parent.children.push(node)
    else roots.push(node)
  }

  const unassigned: OrgNode[] = []
  for (const m of members) {
    const memberNode: OrgNode = {
      key: `m${m.id}`,
      kind: 'member',
      label: m.name,
      member: m,
      children: [],
    }
    const dept = nodes.get(m.departmentId)
    if (dept) dept.children.push(memberNode)
    else unassigned.push(memberNode)
  }
  if (unassigned.length > 0) {
    roots.push({
      key: 'd-unassigned',
      kind: 'department',
      label: unassignedLabel,
      children: unassigned,
    })
  }

  sortForest(roots)
  return roots
}

// 部门在人前面，同类之间按 sortOrder 再按名字。
// 一棵每次刷新顺序都不一样的树，看的人会以为组织变了。
function sortForest(nodes: OrgNode[]) {
  nodes.sort(compareNodes)
  for (const n of nodes) sortForest(n.children)
}

function compareNodes(a: OrgNode, b: OrgNode): number {
  if (a.kind !== b.kind) return a.kind === 'department' ? -1 : 1
  if (a.kind === 'department') {
    const sa = a.department?.sortOrder ?? 0
    const sb = b.department?.sortOrder ?? 0
    if (sa !== sb) return sa - sb
  }
  return a.label.localeCompare(b.label, 'zh-Hans-CN')
}

/**
 * 以某个人为中心看到的一圈人。
 *
 * 组织架构图不画全公司——三百个方块铺满屏幕，等于什么都没说。每次只回答
 * 「**这个人**在组织里的位置」：他上面一路是谁，身边是谁，下面是谁。点谁
 * 就换成谁，一层层走过去。
 */
export interface FocusView {
  /** 上级链，从最高一层排到直属上级。没有上级时是空的。 */
  ancestors: OrgMember[]
  /** 中心那个人。找不到时是 undefined，页面据此显示「找不到这个人」。 */
  focus?: OrgMember
  /**
   * 和中心同一个上级的人，**包含中心自己**，按名字排序。
   *
   * 包含自己是有意的：这一行画出来就是「你和你的同事」，把自己抽掉会让
   * 中心那个方块无处安放，也让「你在这一排的第几个」这个信息消失。
   */
  peers: OrgMember[]
  /** 直接下属。 */
  reports: OrgMember[]
}

/**
 * 三处会走错的地方，和 buildReportingTree 是同一批：
 *
 *  1. 上级不在名单里（多半已离职）——链子到此为止，不是崩掉
 *  2. 成环——沿链上溯要有步数上限，否则死循环
 *  3. 中心自己不在名单里——返回空视图，让页面能说「找不到」
 */
export function focusView(members: OrgMember[], focusId: string): FocusView {
  const byID = new Map<string, OrgMember>()
  for (const m of members) byID.set(m.id, m)
  const focus = byID.get(focusId)
  if (!focus) return { ancestors: [], peers: [], reports: [] }

  // 往上走，边走边记走过谁——重复出现就是环，停。
  const ancestors: OrgMember[] = []
  const walked = new Set<string>([focus.id])
  let cur: OrgMember | undefined = hasManager(focus) ? byID.get(focus.managerId) : undefined
  while (cur && !walked.has(cur.id) && ancestors.length < 32) {
    ancestors.unshift(cur)
    walked.add(cur.id)
    cur = hasManager(cur) ? byID.get(cur.managerId) : undefined
  }

  // 同级 = 和中心挂在同一个上级下面的人。中心头上没人时，同级就是所有
  // 头上没人的人——CEO、独立顾问、还没配上级的新人，他们确实是同一层。
  const managerID = hasManager(focus) && byID.has(focus.managerId) ? focus.managerId : ''
  const peers = members
    .filter((m) => (managerID ? m.managerId === managerID : !hasManager(m) || !byID.has(m.managerId)))
    .sort(byName)

  const reports = members.filter((m) => m.managerId === focus.id && m.id !== focus.id).sort(byName)
  return { ancestors, focus, peers, reports }
}

function byName(a: OrgMember, b: OrgMember): number {
  return a.name.localeCompare(b.name, 'zh-Hans-CN')
}

/** 这一批数据里有多少个人——用来核对「图上画了几个」，见页面上的提示。 */
export function countMembers(nodes: OrgNode[]): number {
  let n = 0
  for (const node of nodes) {
    if (node.kind === 'member') n++
    n += countMembers(node.children)
  }
  return n
}

/** 搜索时判断一个节点要不要留下。空关键词一律留下。 */
export function nodeMatches(node: OrgNode, keyword: string): boolean {
  const q = keyword.trim().toLowerCase()
  if (!q) return true
  if (node.label.toLowerCase().includes(q)) return true
  const m = node.member
  if (!m) return false
  return [m.code, m.englishName, m.position, m.departmentName].some((v) =>
    (v ?? '').toLowerCase().includes(q),
  )
}
