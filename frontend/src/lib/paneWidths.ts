// 三栏的宽度：文件夹栏 | 列表栏 | 阅读区。
//
// 为什么要能手动拖：这三栏没有一个「对」的宽度。同一个人，找一封老信的时候
// 想把列表拉宽（主题看得全），读一封带表格的信的时候想把列表收窄（正文不折
// 行）。桌面上的邮件客户端——Foxmail、Outlook、Apple Mail——全都让人自己拖，
// 因为这件事只有当下的那个人知道答案。
//
// 拖出来的数存在 localStorage，按栏各存一个。**没拖过就一个数都不存**：
// 默认宽度在 CSS 里，列表那一栏是 clamp(280px, 34%, 400px)——按屏幕比例走，
// 换一块屏幕它就该跟着变。一旦写成 px 存下来，这个自适应就没了，所以只有
// 人真的拖过才写。

export type Col = 'rail' | 'list'

/** 每一栏自己的上下限。太窄的列表只剩省略号，太宽的文件夹栏是一片空白。 */
export const LIMITS: Record<Col, { min: number; max: number }> = {
  rail: { min: 150, max: 380 },
  list: { min: 240, max: 640 },
}

/** 阅读区至少留这么宽。比它再窄，一行正文就只剩三四个词。 */
export const READER_MIN = 320

// 分隔条自己也占地方：CSS 里文件夹栏那条是 4 + 10 + 4，列表那条是 3 + 10 + 3
// （留白 + 条 + 留白）。算"还剩多少"的时候要减掉，不然"给阅读区留 320"会少
// 留这几十像素——量出来的确实是 286。
const SEP_RAIL = 18
const SEP_LIST = 16

// 拖一栏的时候，右边还要装下什么。
export const RESERVE: Record<Col, number> = {
  // 文件夹栏右边：一条分隔条、列表的下限、另一条分隔条、阅读区的下限。
  rail: SEP_RAIL + LIMITS.list.min + SEP_LIST + READER_MIN,
  // 列表的 room 量的是「列表 + 分隔条 + 阅读区」那一整块，所以只剩一条。
  list: SEP_LIST + READER_MIN,
}

/**
 * 把一个宽度收进可用范围。
 *
 * room 是这一栏所在的那块地方有多宽：拖文件夹栏时是整个信箱区，拖列表时是
 * 列表和阅读区合起来那块。窗口被缩小之后，上次拖出来的数可能已经装不下了
 * ——那时让位的是这一栏，而不是被挤到没有的阅读区。
 */
export function clampCol(col: Col, px: number, room = Infinity): number {
  const { min, max } = LIMITS[col]
  const roomy = Math.max(min, room - RESERVE[col])
  return Math.round(Math.min(Math.max(px, min), Math.min(max, roomy)))
}

function keyOf(col: Col): string {
  return `mailCol.${col}`
}

/** 存过的宽度。没存过、或者存进去的不是个正常的数，都当没存过。 */
export function readWidth(col: Col, store: Pick<Storage, 'getItem'>): number | null {
  const raw = store.getItem(keyOf(col))
  if (!raw) return null
  const n = Number(raw)
  if (!Number.isFinite(n) || n <= 0) return null
  return clampCol(col, n)
}

export function writeWidth(col: Col, px: number, store: Pick<Storage, 'setItem'>): void {
  store.setItem(keyOf(col), String(Math.round(px)))
}

/** 双击分隔条：忘掉这个数，回到 CSS 里那个会跟着屏幕变的默认值。 */
export function clearWidth(col: Col, store: Pick<Storage, 'removeItem'>): void {
  store.removeItem(keyOf(col))
}
