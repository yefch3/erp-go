// 往下滚就接着加载，不再翻页。
//
// 抽出来的只有「该不该再取一页」这一个判断。它看着像一行 if，但它决定的是
// 一个会**自动重复**的动作：判断松一点，滚动条一停在底下就会一秒钟发十几次
// 请求；判断紧一点，列表到某个位置就再也不动了，而屏幕上什么都不会说。
// 两种坏法都不会报错，所以逐条钉在这里。

/** 列表此刻的样子，够这个判断用的那几样。 */
export interface MoreState {
  /** 服务端还给了下一页的游标：还有东西可取。 */
  hasMore: boolean
  /** 整份列表正在重新拉（换文件夹、换排序、新信到了）。 */
  loading: boolean
  /** 已经在取下一页了。 */
  loadingMore: boolean
  /** 上一次取下一页失败了，正等着人点重试。 */
  failed: boolean
  /** 这份列表支不支持往下接（表格类的不支持，它们还是翻页）。 */
  supported: boolean
}

/**
 * 现在该不该去取下一页。
 *
 * 四个「不该」各挡一件事：
 *
 *   · **没有下一页了**——这是终点，再问也没有。
 *   · **整份列表正在重新拉**——那一次会把列表换掉，这时候接上去的一页属于
 *     上一份名单，接上去就是两份数据混在一起。
 *   · **已经在取了**——哨兵元素露在底下的那一瞬间可能触发好几次（滚动过程中
 *     它反复进出视口），不挡住就是同一页取好几遍。
 *   · **上一次失败了**——自动重试会变成对着一个坏掉的接口每秒敲一次。失败之后
 *     停下来，把重试交给人点。
 */
export function shouldLoadMore(s: MoreState): boolean {
  return s.supported && s.hasMore && !s.loading && !s.loadingMore && !s.failed
}

/**
 * 列表底下此刻该显示什么。
 *
 * 「什么都不显示」也是一种答案，而且是常态：还有下一页、正在往下滚的时候，
 * 底下不该有任何东西——加载提示一闪一闪比没有更吵。
 */
export type MoreStatus = 'none' | 'loading' | 'failed' | 'end'

export function moreStatus(s: MoreState, loadedAny: boolean): MoreStatus {
  if (!s.supported) return 'none'
  if (s.failed) return 'failed'
  if (s.loadingMore) return 'loading'
  // 到底了才说「共 N 封」。整份列表还在拉的时候不说——那一刻行数是 0，
  // 说「共 0 封」是假的。
  if (!s.hasMore && !s.loading && loadedAny) return 'end'
  return 'none'
}
