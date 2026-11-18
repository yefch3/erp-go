// 历史补录采购单的「采购员」下拉和换人（#643）。
//
// 采购单按采购员决定谁看得见。补录常常是一个人统一录、再分给真正负责的采购，
// 所以补录时可以选采购员，录完也可以换。规则都在采购服务里（谁看得见这张单、
// 谁接得住）；这里只有页面上那几步推导，抽出来是为了能在没有浏览器的测试里跑
// ——页面组件本身单测覆盖不到。
import { hasId } from './protoId'

/** 候选名单里的一位同事：持有「维护采购订单」权限的在职员工。 */
export interface BuyerCandidate {
  employeeId: string
  name: string
  departmentName?: string
}

export interface BuyerOption {
  value: string
  label: string
}

/** 下拉里一位同事怎么写。带上部门，重名的时候分得开。 */
export function buyerLabel(candidate: Pick<BuyerCandidate, 'name' | 'departmentName'>): string {
  const department = (candidate.departmentName ?? '').trim()
  return department ? `${candidate.name} · ${department}` : candidate.name
}

/**
 * 补录窗口里「采购员」下拉的选项 = 候选名单，外加「现在这位」。
 *
 * 现在的采购员可能已经不在名单里了——离职了，或者被收了权限——但单子上写的
 * 还是他。不把他放进选项，下拉框就把一串数字编号显示给人看，所以把他补在最
 * 前面。
 *
 * 补的这一项**照样能选**：选回他等于「不换」，服务端也当它什么都没变。标成
 * 不可选的话，人一旦点了别人就再也点不回来，只能关掉窗口重来。
 */
export function buyerOptions(
  candidates: readonly BuyerCandidate[],
  current: { id: string; name: string } | null,
): BuyerOption[] {
  const options: BuyerOption[] = candidates.map((candidate) => ({
    value: String(candidate.employeeId),
    label: buyerLabel(candidate),
  }))
  if (current && hasId(current.id) && !options.some((option) => option.value === String(current.id))) {
    return [{ value: String(current.id), label: current.name || String(current.id) }, ...options]
  }
  return options
}

/**
 * 「更换采购员」里「换给谁」的选项：候选名单去掉现在这位。
 *
 * 和上面那个下拉不一样的地方：这里选的是**新**的人，一开始是空的，现在这位的
 * 名字窗口里另有一栏写着。所以不需要把他补进来；他要是还在名单里也拿掉——
 * 换给自己不算换，留着只是多一个选了没用的选项。
 */
export function handOverOptions(
  candidates: readonly BuyerCandidate[],
  currentId: string | undefined,
): BuyerOption[] {
  const current = hasId(currentId) ? String(currentId) : ''
  return buyerOptions(candidates, null).filter((option) => option.value !== current)
}

/**
 * 选的这位和现在的采购员是不是两个人。
 *
 * 没选（'' / '0' / undefined）不算换人：编号是从接口来的，没值时是字符串 "0"
 * 而不是空（见 protoId.ts），直接比字符串会把「没选」当成「换成了 0 号员工」。
 */
export function isBuyerChange(selectedId: string | undefined, currentId: string | undefined): boolean {
  if (!hasId(selectedId)) return false
  return String(selectedId) !== String(currentId ?? '')
}

/**
 * 失败里能给人看的那句话。
 *
 * api.ts 的拦截器把服务端的信封 `{ success:false, code, message }` 原样 reject
 * 出来，网络层的错误则是 Error；两种都有 message。什么都没有就用调用方给的兜底。
 */
export function buyerChangeFailure(err: unknown, fallback: string): string {
  if (err && typeof err === 'object' && 'message' in err) {
    const message = (err as { message?: unknown }).message
    if (typeof message === 'string' && message.trim()) return message.trim()
  }
  return fallback
}
