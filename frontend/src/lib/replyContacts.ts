// 回信时按地址去通讯录补名字。
//
// 回信的收件人是从原信的 From 头填的（EmailComposer.openReply），那一栏常常
// 只有地址、没名字，或者名字是「sales」这种。收件人没名字，模板里的
// {{contact_name}} 就填不上，发送被拦，人还得手动打名字。通讯录里有这个
// 人的话，名字和公司该从那儿来——那是我们自己维护的记录，比对方邮件客户端
// 随手填的可靠。
//
// 这里是纯规则；查通讯录、改格子在 EmailComposer 里。

/** 收件人格子和通讯录记录共有的那几项。Recipient（RecipientField）满足它。 */
export interface AddressedPerson {
  contactId?: string
  name: string
  email: string
  customerId?: string
  customerName: string
}

/** 回复全部抄了一大串人时也不该把通讯录问穿：够填常见的那几个就行。 */
const MAX_LOOKUPS = 20

function norm(s: string | undefined): string {
  return (s ?? '').trim().toLowerCase()
}

// 通讯录按关键字模糊搜（ILIKE），拿 hans@acme.com 去搜，johans@acme.com 也会
// 回来。只认地址完全相同的。
export function bookHitsFor(email: string, contacts: AddressedPerson[]): AddressedPerson[] {
  const want = norm(email)
  if (!want) return []
  return contacts.filter((c) => norm(c.email) === want)
}

// 把通讯录命中的信息填进收件人格子。
//
// 名字以通讯录为准，通讯录没写名字才留原信头上的。
//
// 同一个地址在通讯录里出现不止一次（一个人挂在两家客户下）：名字一致就用，
// 公司不猜——填错一家比空着更糟，模板里的 {{company_name}} 会把错的公司
// 写进信里，而空着只是发不出去。
export function withBookDetails<T extends AddressedPerson>(chip: T, hits: AddressedPerson[]): T {
  if (hits.length === 0) return chip
  const names = new Set(hits.map((h) => norm(h.name)).filter(Boolean))
  const agreed = names.size === 1 ? hits.find((h) => norm(h.name))?.name.trim() ?? '' : ''
  const named = { ...chip, name: agreed || chip.name }
  if (hits.length !== 1) return named
  const [hit] = hits
  return { ...named, contactId: hit.contactId, customerId: hit.customerId, customerName: hit.customerName }
}

// 哪些格子要去问通讯录：有地址、还没配上通讯录（没有 contactId）的，去重。
export function addressesToLookUp(chips: AddressedPerson[]): string[] {
  const seen = new Set<string>()
  const out: string[] = []
  for (const chip of chips) {
    const email = norm(chip.email)
    if (!email || chip.contactId || seen.has(email)) continue
    seen.add(email)
    out.push(email)
    if (out.length >= MAX_LOOKUPS) break
  }
  return out
}
