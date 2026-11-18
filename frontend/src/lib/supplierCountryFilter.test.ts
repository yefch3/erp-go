import { describe, expect, it } from 'vitest'

import {
  UNCLASSIFIED_COUNTRY,
  countryChoices,
  describeCountrySelection,
  totalInChoices,
  withSelectedChoice,
} from './supplierCountryFilter'

describe('countryChoices', () => {
  it('有国家码的分组原样成为选项，顺序不变', () => {
    expect(countryChoices([{ countryCode: 'CN', count: '1' }, { countryCode: 'VN', count: '2' }])).toEqual([
      { value: 'CN', code: 'CN', count: 1 },
      { value: 'VN', code: 'VN', count: 2 },
    ])
  })

  // 这就是那个选不出来的组。它的取值不能是空串——空串是「全部国家」那一项的
  // 取值，两项一样的话选哪个都等于没选。
  it('没填国家的那一组用专用取值，而不是和「全部国家」一样的空串', () => {
    const [choice] = countryChoices([{ countryCode: '', count: '3' }])

    expect(choice).toEqual({ value: UNCLASSIFIED_COUNTRY, code: '', count: 3 })
    expect(choice.value).not.toBe('')
    // 发给服务端的就是这个取值，和客户列表、主数据服务是同一个约定。
    expect(UNCLASSIFIED_COUNTRY).toBe('__UNCLASSIFIED__')
  })

  // 国家码在库里是定长两位，没填的会被补成两个空格。服务端现在去掉空格再给，
  // 但页面比服务先上线的那一小段时间里拿到的还是两个空格——照样认得出来。
  it('国家码是两个空格的分组也算没填国家', () => {
    expect(countryChoices([{ countryCode: '  ', count: '1' }])).toEqual([
      { value: UNCLASSIFIED_COUNTRY, code: '', count: 1 },
    ])
  })

  it('接口没带国家码字段时同样算没填国家', () => {
    expect(countryChoices([{ count: 4 }])).toEqual([{ value: UNCLASSIFIED_COUNTRY, code: '', count: 4 }])
  })

  it('去掉空格后是同一个国家码的分组并成一项，家数相加', () => {
    expect(
      countryChoices([
        { countryCode: '', count: '1' },
        { countryCode: 'VN', count: '2' },
        { countryCode: '  ', count: '5' },
        { countryCode: 'VN ', count: '1' },
      ]),
    ).toEqual([
      { value: UNCLASSIFIED_COUNTRY, code: '', count: 6 },
      { value: 'VN', code: 'VN', count: 3 },
    ])
  })

  it('家数不是数字时按 0 算，不让下拉里出现 NaN', () => {
    expect(countryChoices([{ countryCode: 'DE', count: 'abc' }, { countryCode: 'CN' }])).toEqual([
      { value: 'DE', code: 'DE', count: 0 },
      { value: 'CN', code: 'CN', count: 0 },
    ])
  })

  it('接口什么都没给时是空列表', () => {
    expect(countryChoices(undefined)).toEqual([])
    expect(countryChoices([])).toEqual([])
  })
})

// 下拉框对「选中的取值不在选项里」的处理是把取值原样显示出来。对具体国家是
// 「VN」，还说得过去；对没填国家这一组，就是把 __UNCLASSIFIED__ 这串内部标记
// 直接亮给用户看。而这恰恰是最常见的用法会走到的地方：顺着「未分类」把供应商
// 一家家补上国家，补完最后一家返回列表——地址栏里还是这一组，分组却已经没了。
describe('withSelectedChoice', () => {
  const choices = countryChoices([{ countryCode: 'CN', count: '1' }, { countryCode: 'VN', count: '2' }])

  it('选中的是没填国家那一组、而这一组已经空了：补一个家数为 0 的选项', () => {
    expect(withSelectedChoice(choices, UNCLASSIFIED_COUNTRY)).toEqual([
      ...choices,
      { value: UNCLASSIFIED_COUNTRY, code: '', count: 0 },
    ])
  })

  it('选中的国家现在一家供应商都没有：同样补上，名字才显示得出来', () => {
    expect(withSelectedChoice(choices, 'BR')).toEqual([...choices, { value: 'BR', code: 'BR', count: 0 }])
  })

  it('分组还没加载出来时，选中的那一项也先有个选项', () => {
    expect(withSelectedChoice([], UNCLASSIFIED_COUNTRY)).toEqual([{ value: UNCLASSIFIED_COUNTRY, code: '', count: 0 }])
  })

  it('选中的本来就在选项里，或者什么都没选：原样返回，不多出东西', () => {
    expect(withSelectedChoice(choices, 'VN')).toEqual(choices)
    expect(withSelectedChoice(choices, '')).toEqual(choices)
  })

  it('补出来的选项不改变总家数，也不动传进来的列表', () => {
    const before = [...choices]
    const patched = withSelectedChoice(choices, UNCLASSIFIED_COUNTRY)

    expect(totalInChoices(patched)).toBe(totalInChoices(choices))
    expect(choices).toEqual(before)
    expect(describeCountrySelection(patched, UNCLASSIFIED_COUNTRY)).toEqual({ kind: 'unclassified', code: '', count: 0 })
  })
})

describe('totalInChoices', () => {
  it('把各组家数加起来，没填国家的那一组也算在内', () => {
    const choices = countryChoices([{ countryCode: '', count: '1' }, { countryCode: 'VN', count: '2' }, { countryCode: 'CN', count: '1' }])
    expect(totalInChoices(choices)).toBe(4)
  })
})

describe('describeCountrySelection', () => {
  const choices = countryChoices([
    { countryCode: '', count: '1' },
    { countryCode: 'CN', count: '1' },
    { countryCode: 'VN', count: '2' },
  ])

  it('没选国家：全部，家数是各组之和', () => {
    expect(describeCountrySelection(choices, '')).toEqual({ kind: 'all', code: '', count: 4 })
  })

  it('选了具体国家：那个国家和它的家数', () => {
    expect(describeCountrySelection(choices, 'VN')).toEqual({ kind: 'country', code: 'VN', count: 2 })
  })

  // 以前这里顶上显示成「 · 1」：名字是空的。
  it('选了没填国家的那一组：能认出来是「未分类」，家数是那一组的', () => {
    expect(describeCountrySelection(choices, UNCLASSIFIED_COUNTRY)).toEqual({ kind: 'unclassified', code: '', count: 1 })
  })

  it('地址栏里带着一个当前没有供应商的国家：照样显示那个国家，家数为 0', () => {
    expect(describeCountrySelection(choices, 'BR')).toEqual({ kind: 'country', code: 'BR', count: 0 })
  })

  it('分组还没加载出来时不报错', () => {
    expect(describeCountrySelection([], UNCLASSIFIED_COUNTRY)).toEqual({ kind: 'unclassified', code: '', count: 0 })
    expect(describeCountrySelection([], '')).toEqual({ kind: 'all', code: '', count: 0 })
  })
})
