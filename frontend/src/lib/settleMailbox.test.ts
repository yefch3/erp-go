import { describe, expect, it } from 'vitest'
import { settleMailbox } from './mailUnlock'

const boxes = [{ id: 11 }, { id: 23 }]

describe('settleMailbox：信箱清单到了之后站在哪个箱上', () => {
  it('还没选过（0）→ 默认箱，也就是清单第一个', () => {
    expect(settleMailbox(0, boxes)).toBe(11)
  })
  it('选的箱在清单里 → 不动', () => {
    expect(settleMailbox(23, boxes)).toBe(23)
  })
  it('选的箱不是这个人的（旧令牌被读成 1 号箱）→ 落回默认箱，不能卡在空视图上', () => {
    expect(settleMailbox(1, boxes)).toBe(11)
  })
  it('清单为空 → 0，交给调用方摆门', () => {
    expect(settleMailbox(1, [])).toBe(0)
    expect(settleMailbox(0, [])).toBe(0)
  })
})
