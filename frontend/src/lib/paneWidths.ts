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

// 分隔条自己占的宽度：CSS 里文件夹栏那条是 4 + 10 + 4，列表那条是 3 + 10 + 3
// （留白 + 条 + 留白）。算「这一栏还能拖到多宽」时要把它们减掉，否则拖到头
// 会把右边那栏挤出容器，整页多出一条横向滚动条。
export const SEP_RAIL = 18
export const SEP_LIST = 16

/**
 * 把一个宽度收进能用的范围。
 *
 * **一条下限都不设。** 从前这里给每栏写了最小宽度（文件夹栏 150、列表 240、
 * 阅读区 320），拖到那儿就拖不动了——而那三个数是凭空定的：有人就是想把
 * 列表收成一条窄缝只看时间，也有人想把它拉到半屏当表格用。多窄算窄是拖的
 * 人自己的事，代码不该替他决定。拖没了也有回去的路：双击分隔条回默认。
 *
 * 剩下两条边是硬的，因为它们不是偏好：
 * · 0——负数不是宽度；
 * · room——这一栏所在的那块地方有多宽（拖文件夹栏时是整个信箱区，拖列表时
 *   是「列表 + 分隔条 + 阅读区」那一块，两者都已经减掉分隔条）。再往外拖
 *   就是把右边那栏挤出容器，换来一条谁都不想要的横向滚动条。
 */
export function clampCol(px: number, room = Infinity): number {
  return Math.round(Math.min(Math.max(px, 0), Math.max(0, room)))
}

function keyOf(col: Col): string {
  return `mailCol.${col}`
}

/** 存过的宽度。没存过、或者存进去的不是个正常的数，都当没存过。 */
export function readWidth(col: Col, store: Pick<Storage, 'getItem'>): number | null {
  const raw = store.getItem(keyOf(col))
  if (!raw) return null
  const n = Number(raw)
  if (!Number.isFinite(n) || n < 0) return null
  // 这里不收进 room：读的时候还没有 DOM，量不到屏幕。装不装得下由页面上的
  // refitCols 在画出来之后说。
  return Math.round(n)
}

export function writeWidth(col: Col, px: number, store: Pick<Storage, 'setItem'>): void {
  store.setItem(keyOf(col), String(Math.round(px)))
}

/** 双击分隔条：忘掉这个数，回到 CSS 里那个会跟着屏幕变的默认值。 */
export function clearWidth(col: Col, store: Pick<Storage, 'removeItem'>): void {
  store.removeItem(keyOf(col))
}
