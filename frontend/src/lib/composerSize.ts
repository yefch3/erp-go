// 写信窗停靠在右下角时的大小：人拖多大就多大，记在浏览器里。
//
// 窗子贴着右下角，所以能拖的是左边、上边和左上角：往左拖变宽，往上拖变高。
// 这里是纯算术；指针事件、存取在 EmailComposer 里。

export interface Size {
  w: number
  h: number
}

/** 拖的是哪条边。n = 上边，w = 左边，nw = 左上角。 */
export type ResizeEdge = 'n' | 'w' | 'nw'

/** 停靠窗默认大小，和从前 CSS 里写死的那两个数一样。 */
export const COMPOSER_DEFAULT: Size = { w: 620, h: 640 }

/** 再小就放不下收件人、主题和几行正文，拖到这里停。 */
export const COMPOSER_MIN: Size = { w: 420, h: 360 }

/** 窗子离屏幕边缘留的量：右边 24（.is-dock 的 right）+ 左边至少再留 24。 */
export const COMPOSER_MARGIN = 48

const STORAGE_KEY = 'mail.composerSize'

/** 夹在「最小」和「屏幕减去边距」之间。屏幕比最小值还小时以屏幕为准。 */
export function clampComposerSize(size: Size, viewport: Size): Size {
  const maxW = Math.max(COMPOSER_MIN.w, viewport.w - COMPOSER_MARGIN)
  const maxH = Math.max(COMPOSER_MIN.h, viewport.h - COMPOSER_MARGIN)
  return {
    w: Math.min(maxW, Math.max(COMPOSER_MIN.w, Math.round(size.w))),
    h: Math.min(maxH, Math.max(COMPOSER_MIN.h, Math.round(size.h))),
  }
}

/**
 * 从拖动开始时的大小和指针位移算出新大小。
 * dx、dy 是指针相对按下那一点的位移（向右、向下为正）。
 */
export function resizedSize(start: Size, edge: ResizeEdge, dx: number, dy: number, viewport: Size): Size {
  const w = edge === 'n' ? start.w : start.w - dx
  const h = edge === 'w' ? start.h : start.h - dy
  return clampComposerSize({ w, h }, viewport)
}

type Store = Pick<Storage, 'getItem' | 'setItem'>

/** 读上次记的大小；没记过、记坏了都回默认。 */
export function readComposerSize(storage: Store | null | undefined): Size {
  try {
    const raw = storage?.getItem(STORAGE_KEY)
    if (!raw) return COMPOSER_DEFAULT
    const parsed = JSON.parse(raw) as Partial<Size>
    if (typeof parsed.w !== 'number' || typeof parsed.h !== 'number') return COMPOSER_DEFAULT
    if (!Number.isFinite(parsed.w) || !Number.isFinite(parsed.h)) return COMPOSER_DEFAULT
    return { w: parsed.w, h: parsed.h }
  } catch {
    return COMPOSER_DEFAULT
  }
}

/** 记下来。无痕模式记不住不是错。 */
export function writeComposerSize(storage: Store | null | undefined, size: Size): void {
  try {
    storage?.setItem(STORAGE_KEY, JSON.stringify({ w: size.w, h: size.h }))
  } catch {
    /* 记不住就算了 */
  }
}
