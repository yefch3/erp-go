import { describe, expect, it } from 'vitest'
import { approvalPriorityTagType, approvalSourceLink, approvalTimeAmount } from './homeApproval'

describe('approvalSourceLink', () => {
  it('将合同审批定位到合同详情', () => {
    expect(approvalSourceLink({ bizType: 'CONTRACT', bizId: '12' })).toEqual({
      path: '/contracts', query: { id: '12' },
    })
  })

  it('将采购审批和供应商差异确认定位到采购单详情', () => {
    expect(approvalSourceLink({ bizType: 'PURCHASE_ORDER', bizId: '21' })).toEqual({
      path: '/purchase-orders', query: { order: '21' },
    })
    expect(approvalSourceLink({ bizType: 'PURCHASE_ORDER_CHANGE', bizId: '22' })).toEqual({
      path: '/purchase-orders', query: { order: '22' },
    })
  })

  it('将物流方案确认定位到实单询价详情', () => {
    expect(approvalSourceLink({ bizType: 'SHIPPING_REQUOTE', bizId: '31' })).toEqual({
      path: '/shipping/requirements', query: { handoff: '31' },
    })
  })

  it('未知业务类型不生成猜测链接', () => {
    expect(approvalSourceLink({ bizType: 'UNKNOWN', bizId: '99' })).toBeNull()
  })
})

describe('approval urgency presentation', () => {
  it('按紧急等级选择稳定颜色', () => {
    expect(approvalPriorityTagType('OVERDUE')).toBe('danger')
    expect(approvalPriorityTagType('URGENT')).toBe('danger')
    expect(approvalPriorityTagType('HIGH')).toBe('warning')
    expect(approvalPriorityTagType('NORMAL')).toBe('info')
  })

  it('将剩余或超时分钟转换成易读单位', () => {
    expect(approvalTimeAmount(1500)).toEqual({ overdue: false, unit: 'days', count: 2 })
    expect(approvalTimeAmount(61)).toEqual({ overdue: false, unit: 'hours', count: 2 })
    expect(approvalTimeAmount(-30)).toEqual({ overdue: true, unit: 'minutes', count: 30 })
  })
})
