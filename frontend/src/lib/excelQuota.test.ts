import { describe, expect, it } from 'vitest'

import {
  emptyExcelQuota,
  excelQuotaPercent,
  excelQuotaProgressStatus,
  excelQuotaRemaining,
  excelQuotaState,
  excelQuotaTone,
  parseExcelQuota,
  type ExcelQuota,
} from './excelQuota'

function quota(patch: Partial<ExcelQuota> = {}): ExcelQuota {
  return { ...emptyExcelQuota, currentMonth: '2026-08', ...patch }
}

describe('excelQuotaState', () => {
  it('服务端还没答话时说「不知道」，不冒充任何一种确定状态', () => {
    expect(excelQuotaState(emptyExcelQuota, '2026-08')).toBe('unknown')
  })

  it('翻看往月时不给「还剩多少」——那个月已经过完了', () => {
    expect(excelQuotaState(quota({ limited: true, monthlyRuns: 200 }), '2026-07')).toBe('past')
  })

  it('当月没设上限就是不限', () => {
    expect(excelQuotaState(quota(), '2026-08')).toBe('unlimited')
  })

  it('当月且有上限时百分比才是活的', () => {
    expect(excelQuotaState(quota({ limited: true, monthlyRuns: 200 }), '2026-08')).toBe('live')
  })
})

describe('excelQuotaPercent', () => {
  it('普通情况四舍五入到整数', () => {
    expect(excelQuotaPercent(quota({ limited: true, monthlyRuns: 200, usedThisMonth: 41 }))).toBe(21)
  })

  it('上限 0 算作满额，而不是除以 0 得出 NaN', () => {
    const p = excelQuotaPercent(quota({ limited: true, monthlyRuns: 0, usedThisMonth: 0 }))
    expect(Number.isFinite(p)).toBe(true)
    expect(p).toBe(100)
  })

  it('超出上限时如实报大于 100，不悄悄截断', () => {
    expect(excelQuotaPercent(quota({ limited: true, monthlyRuns: 10, usedThisMonth: 12 }))).toBe(120)
  })
})

describe('excelQuotaRemaining', () => {
  it('剩余不会是负数', () => {
    expect(excelQuotaRemaining(quota({ limited: true, monthlyRuns: 10, usedThisMonth: 12 }))).toBe(0)
  })

  it('正常剩余就是差值', () => {
    expect(excelQuotaRemaining(quota({ limited: true, monthlyRuns: 200, usedThisMonth: 41 }))).toBe(159)
  })
})

describe('警戒色', () => {
  it('日常用量不渲染成警告——那是正常在用', () => {
    expect(excelQuotaTone(21)).toBe('info')
    expect(excelQuotaProgressStatus(21)).toBe('')
  })

  it('八成起变黄', () => {
    expect(excelQuotaTone(79)).toBe('info')
    expect(excelQuotaTone(80)).toBe('warning')
    expect(excelQuotaProgressStatus(80)).toBe('warning')
  })

  it('满了变红，超了也是红', () => {
    expect(excelQuotaTone(100)).toBe('error')
    expect(excelQuotaTone(120)).toBe('error')
    expect(excelQuotaProgressStatus(100)).toBe('exception')
  })
})

// 接口真正送到浏览器里的是什么形状——这一组用的都是**实测**的 JSON。
//
// 客户那条 /api/excel-usage 走 protojson，int64 变成字符串；平台那条
// /api/platform/excel-quotas 走普通 JSON，同样的字段是数字。两条路的类型
// 不一样，而页面上写的都是 number。之前靠 JS 隐式转换侥幸没错，
// 这一组把它变成保证。
describe('parseExcelQuota', () => {
  // 实测：go test 里把 ExcelQuota 用 protojson 打出来就是这个样子。
  const fromProtojson = {
    limited: true,
    monthlyRuns: '200',
    usedThisMonth: '41',
    currentMonth: '2026-08',
  }

  it('把 protojson 送来的字符串数字转成真的数字', () => {
    const q = parseExcelQuota(fromProtojson)
    expect(q.monthlyRuns).toBe(200)
    expect(q.usedThisMonth).toBe(41)
    expect(typeof q.monthlyRuns).toBe('number')
    expect(typeof q.usedThisMonth).toBe('number')
  })

  it('转完之后加法是加法，不是拼接', () => {
    const q = parseExcelQuota(fromProtojson)
    // 没转的话这里会得到 "411"——这正是当初埋着的那颗雷。
    expect(q.usedThisMonth + 1).toBe(42)
  })

  it('百分比和剩余次数在字符串输入下也算得对', () => {
    const q = parseExcelQuota(fromProtojson)
    expect(excelQuotaPercent(q)).toBe(21)
    expect(excelQuotaRemaining(q)).toBe(159)
  })

  it('服务端没答话时给出一份干净的空额度，而不是 NaN', () => {
    for (const bad of [undefined, null, 'nonsense', 42]) {
      const q = parseExcelQuota(bad)
      expect(q.limited).toBe(false)
      expect(q.monthlyRuns).toBe(0)
      expect(q.currentMonth).toBe('')
      expect(Number.isNaN(q.monthlyRuns)).toBe(false)
    }
  })

  it('字段缺失或解不出数就当 0，绝不让 NaN 流到页面上', () => {
    const q = parseExcelQuota({ limited: true, monthlyRuns: '不是数字' })
    expect(q.monthlyRuns).toBe(0)
    expect(q.usedThisMonth).toBe(0)
    // 上限 0 的含义是「一次都不许用」，所以百分比该是 100 而不是 NaN。
    expect(excelQuotaPercent(q)).toBe(100)
  })

  it('limited 只认真正的 true——字符串 "false" 不该被当成有上限', () => {
    expect(parseExcelQuota({ limited: 'false' }).limited).toBe(false)
    expect(parseExcelQuota({ limited: true }).limited).toBe(true)
  })
})
