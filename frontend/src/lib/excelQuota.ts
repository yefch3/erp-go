// 智能转换额度在页面上的算法：百分比、警戒色、以及「这张卡该说哪句话」。
//
// 抽出来单独放，是因为这几条判断的边角比它们看起来多——上限 0、已用超过
// 上限、翻看往月、服务端还没答话——而每一条错了都会在页面上变成一句确凿
// 的假话。放在 .vue 里没法测，放在这里可以。

export interface ExcelQuota {
  /** 有没有上限。false 时 monthlyRuns 没有意义。 */
  limited: boolean
  monthlyRuns: number
  usedThisMonth: number
  /** 数据库认定的当前月份 YYYY-MM；空串表示服务端还没答话。 */
  currentMonth: string
}

export const emptyExcelQuota: ExcelQuota = {
  limited: false,
  monthlyRuns: 0,
  usedThisMonth: 0,
  currentMonth: '',
}

/**
 * 这张额度卡该说哪句话。
 *
 * - `unknown` —— 服务端还没答话。这时候既不能说「未设上限」也不能说「往月
 *   记录」：两句都是断言，而我们什么都还不知道。
 * - `past` —— 用户翻的是过去的月份。「还剩多少」对一个过完的月没有意义。
 * - `unlimited` —— 当月，但这家公司没有上限。
 * - `live` —— 当月且有上限，百分比是一个活的事实。
 */
export function excelQuotaState(
  quota: ExcelQuota,
  viewingMonth: string,
): 'unknown' | 'past' | 'unlimited' | 'live' {
  if (!quota.currentMonth) return 'unknown'
  if (viewingMonth !== quota.currentMonth) return 'past'
  return quota.limited ? 'live' : 'unlimited'
}

/**
 * 已用百分比，四舍五入到整数。
 *
 * 上限 0 表示「一次都不许用」——除以 0 得不出数，直接算 100%：一个用不了
 * 的额度就是满的。超出上限时返回的数会大于 100，交给调用方决定是显示真实
 * 数字还是把进度条截在 100。
 */
export function excelQuotaPercent(quota: ExcelQuota): number {
  if (quota.monthlyRuns <= 0) return 100
  return Math.round((quota.usedThisMonth / quota.monthlyRuns) * 100)
}

/** 剩余次数，不会是负数——「还剩 -3 次」不是一句人话。 */
export function excelQuotaRemaining(quota: ExcelQuota): number {
  return Math.max(quota.monthlyRuns - quota.usedThisMonth, 0)
}

/** 80% 起变黄，满了变红。低于 80% 不渲染成警告——那是正常在用。 */
export function excelQuotaTone(percent: number): 'info' | 'warning' | 'error' {
  if (percent >= 100) return 'error'
  if (percent >= 80) return 'warning'
  return 'info'
}

/** el-progress 的 status，同一组阈值。'' 表示默认蓝色。 */
export function excelQuotaProgressStatus(percent: number): '' | 'warning' | 'exception' {
  if (percent >= 100) return 'exception'
  if (percent >= 80) return 'warning'
  return ''
}
