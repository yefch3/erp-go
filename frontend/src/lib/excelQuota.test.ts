import { describe, expect, it } from 'vitest'

import {
  emptyExcelQuota,
  excelQuotaPercent,
  excelQuotaProgressStatus,
  excelQuotaRemaining,
  excelQuotaState,
  excelQuotaTone,
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
