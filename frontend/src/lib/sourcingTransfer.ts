import { hasId } from './protoId'

/**
 * 询盘联系人是可选的，但 protojson 的 int64 字段不接受空字符串。
 * Element Plus 清空 select 后给的是 ""，因此没有真实联系人时省略整个字段；
 * 选了联系人则保留字符串形态，避免大整数 id 经过 Number 后丢精度。
 */
export function optionalSourcingContact(
  contactId: string | number | null | undefined,
): { contactId?: string } {
  if (!hasId(contactId)) return {}
  return { contactId: String(contactId).trim() }
}
