import { describe, expect, it } from 'vitest'

import { buyerChangeFailure, buyerLabel, buyerOptions, handOverOptions, isBuyerChange, type BuyerCandidate } from './orderBuyer'

const candidates: BuyerCandidate[] = [
  { employeeId: '19', name: '张晓雅', departmentName: '天津' },
  { employeeId: '21', name: '李晨', departmentName: '浙江' },
  { employeeId: '27', name: '王燕楠', departmentName: '' },
]

describe('buyerLabel', () => {
  it('带上部门，重名的时候分得开；没有部门就只写名字', () => {
    expect(buyerLabel(candidates[0])).toBe('张晓雅 · 天津')
    expect(buyerLabel(candidates[2])).toBe('王燕楠')
    expect(buyerLabel({ name: '李晨' })).toBe('李晨')
    expect(buyerLabel({ name: '李晨', departmentName: '  ' })).toBe('李晨')
  })
})

describe('buyerOptions', () => {
  it('候选名单原样变成选项，顺序不动', () => {
    const options = buyerOptions(candidates, { id: '27', name: '王燕楠' })
    expect(options).toEqual([
      { value: '19', label: '张晓雅 · 天津' },
      { value: '21', label: '李晨 · 浙江' },
      { value: '27', label: '王燕楠' },
    ])
  })

  it('现在的采购员不在名单里（离职、被收了权限）：补在最前面，下拉才不会显示一串编号', () => {
    const options = buyerOptions(candidates, { id: '34', name: '高圣' })
    // 补的这一项照样能选：选回他等于「不换」，服务端也当它什么都没变。
    // 要是标成不可选，人一旦点了别人就再也点不回来，只能关掉窗口重来。
    expect(options[0]).toEqual({ value: '34', label: '高圣' })
    expect(options.map((option) => option.value)).toEqual(['34', '19', '21', '27'])
  })

  it('补录新单时没有「现在这位」：只有候选名单', () => {
    expect(buyerOptions(candidates, null)).toHaveLength(3)
    // 接口没值时给的是字符串 "0"，不是空——不能把它当成一位员工补进去。
    expect(buyerOptions(candidates, { id: '0', name: '' })).toHaveLength(3)
    expect(buyerOptions(candidates, { id: '', name: '' })).toHaveLength(3)
  })

  it('现在这位的名字缺了，至少把编号给人看', () => {
    expect(buyerOptions([], { id: '34', name: '' })[0].label).toBe('34')
  })

  it('编号是数字还是字符串都认成同一个人', () => {
    const numeric = [{ employeeId: 19 as unknown as string, name: '张晓雅' }]
    expect(buyerOptions(numeric, { id: '19', name: '张晓雅' })).toHaveLength(1)
  })
})

describe('handOverOptions', () => {
  it('「换给谁」的名单里没有现在这位——换给自己不算换', () => {
    expect(handOverOptions(candidates, '27')).toEqual([
      { value: '19', label: '张晓雅 · 天津' },
      { value: '21', label: '李晨 · 浙江' },
    ])
  })

  it('现在这位已经不在名单里：名单原样，不把他补进来——他的名字窗口上方另有一栏写着', () => {
    expect(handOverOptions(candidates, '34').map((option) => option.value)).toEqual(['19', '21', '27'])
  })

  it('单子上还没有采购员（存量数据，接口给的是 "0"）：谁都可以选', () => {
    expect(handOverOptions(candidates, '0')).toHaveLength(3)
    expect(handOverOptions(candidates, undefined)).toHaveLength(3)
  })

  it('编号是数字还是字符串都认成同一个人', () => {
    const numeric = [{ employeeId: 19 as unknown as string, name: '张晓雅' }, { employeeId: 21 as unknown as string, name: '李晨' }]
    expect(handOverOptions(numeric, '19').map((option) => option.value)).toEqual(['21'])
    expect(handOverOptions(candidates, 27 as unknown as string).map((option) => option.value)).toEqual(['19', '21'])
  })

  it('名单里只有现在这位：没有人可换', () => {
    expect(handOverOptions([candidates[2]], '27')).toEqual([])
  })
})

describe('isBuyerChange', () => {
  it('选了另一位才算换人', () => {
    expect(isBuyerChange('19', '27')).toBe(true)
    expect(isBuyerChange('27', '27')).toBe(false)
  })

  it('没选不算换人——接口的「没值」是字符串 "0"，直接比字符串会当成换给了 0 号', () => {
    expect(isBuyerChange('', '27')).toBe(false)
    expect(isBuyerChange('0', '27')).toBe(false)
    expect(isBuyerChange(undefined, '27')).toBe(false)
  })

  it('原来没有采购员（存量数据）而现在选了一位：算换人', () => {
    expect(isBuyerChange('19', '0')).toBe(true)
    expect(isBuyerChange('19', undefined)).toBe(true)
  })
})

describe('buyerChangeFailure', () => {
  it('服务端说了原因就用它的，没说就用兜底', () => {
    expect(buyerChangeFailure({ success: false, code: 'PO_BUYER_NOT_ELIGIBLE', message: ' 所选同事已停用 ' }, '兜底')).toBe('所选同事已停用')
    expect(buyerChangeFailure(new Error('Network Error'), '兜底')).toBe('Network Error')
    expect(buyerChangeFailure({ message: '' }, '兜底')).toBe('兜底')
    expect(buyerChangeFailure(null, '兜底')).toBe('兜底')
    expect(buyerChangeFailure('boom', '兜底')).toBe('兜底')
  })
})
