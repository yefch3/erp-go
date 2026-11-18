// 供应商列表上方「国家/地区」那个下拉的规矩：选项怎么来，顶上那行怎么写。
//
// 单独抽出来，是因为「没填国家」的那一组一直是坏的，而且坏得不声不响：
//   · 国家码在库里是定长两位，没填的会被补成两个空格。页面写的是
//     「有国家码就显示国家名，否则显示未分类」，两个空格算「有」，于是这一组
//     的名字成了空白的「（N）」；
//   · 就算名字对了，它的取值也和「全部国家」一样是空串——选它等于没选，
//     下面还是全部供应商。
//
// 现在：国家码先去掉空格；没填国家的那一组用专用取值 UNCLASSIFIED_COUNTRY，
// 页面把它原样发给服务端。客户列表和主数据服务用的是同一个约定。
//
// 页面组件没有单测，所以这些判断放在这里。

/** 「只看没填国家的」在请求里的写法。 */
export const UNCLASSIFIED_COUNTRY = '__UNCLASSIFIED__'

/** 分组接口给的一行。家数是 int64，接口把它写成字符串。 */
export interface CountryCountRow {
  countryCode?: string
  count?: string | number
}

/** 下拉里的一项。 */
export interface CountryChoice {
  /** 选中后发给服务端的取值：国家码，或 UNCLASSIFIED_COUNTRY。 */
  value: string
  /** 国家码；空串表示「没填国家」这一组。 */
  code: string
  count: number
}

function toCount(value: unknown): number {
  const n = Number(value)
  return Number.isFinite(n) ? n : 0
}

/**
 * 把分组接口的结果整理成下拉选项，顺序跟着接口走。
 *
 * 去掉空格后国家码相同的分组并成一项：正常情况下不会有重复，但「两个空格」和
 * 「空串」说的是同一件事，不能在下拉里变成两项。
 */
export function countryChoices(rows: readonly CountryCountRow[] | undefined): CountryChoice[] {
  const byCode = new Map<string, number>()
  for (const row of rows ?? []) {
    const code = String(row?.countryCode ?? '').trim()
    byCode.set(code, (byCode.get(code) ?? 0) + toCount(row?.count))
  }
  return [...byCode].map(([code, count]) => ({ value: code || UNCLASSIFIED_COUNTRY, code, count }))
}

/**
 * 保证当前选中的那一项在选项里；不在就补一个家数为 0 的。
 *
 * 下拉框遇到「选中的取值不在选项里」时，会把取值原样显示出来。对没填国家的
 * 那一组，这意味着把 __UNCLASSIFIED__ 这串内部标记直接亮给用户看——而最常见的
 * 用法恰好会走到这里：顺着「未分类」把供应商一家家补上国家，补完最后一家返回
 * 列表，地址栏里还是这一组，分组却已经没有了。分组还没加载出来的那一瞬间也一样。
 */
export function withSelectedChoice(choices: readonly CountryChoice[], selected: string): CountryChoice[] {
  if (!selected || choices.some((choice) => choice.value === selected)) return [...choices]
  return [...choices, { value: selected, code: selected === UNCLASSIFIED_COUNTRY ? '' : selected, count: 0 }]
}

export function totalInChoices(choices: readonly CountryChoice[]): number {
  return choices.reduce((sum, choice) => sum + choice.count, 0)
}

/** 当前选中的是什么：全部、没填国家的那一组、还是某个国家。 */
export interface CountrySelection {
  kind: 'all' | 'unclassified' | 'country'
  /** kind 为 country 时的国家码，其余为空串。 */
  code: string
  count: number
}

/**
 * 顶上那行「越南 · 2」要用的东西。名字由页面按语言去取，这里只判断是哪一种。
 *
 * selected 来自地址栏时可能是一个现在没有供应商的国家：照样按那个国家显示，
 * 家数为 0。
 */
export function describeCountrySelection(choices: readonly CountryChoice[], selected: string): CountrySelection {
  if (!selected) return { kind: 'all', code: '', count: totalInChoices(choices) }
  const count = choices.find((choice) => choice.value === selected)?.count ?? 0
  if (selected === UNCLASSIFIED_COUNTRY) return { kind: 'unclassified', code: '', count }
  return { kind: 'country', code: selected, count }
}
