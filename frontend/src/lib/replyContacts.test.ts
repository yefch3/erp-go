import { describe, expect, it } from 'vitest'
import { addressesToLookUp, bookHitsFor, withBookDetails, type AddressedPerson } from './replyContacts'

// 回信时收件人是从原信的 From 头填的，常常只有地址没名字，模板里的
// {{contact_name}} 就填不上。这里按地址去通讯录补名字（和公司）。

const hans = { contactId: '11', name: 'Hans Weber', email: 'hans@acme.com', customerId: '5', customerName: 'ACME GmbH' }

describe('通讯录命中', () => {
  it('只认地址完全相同的，不分大小写；模糊搜回来的其它人不算', () => {
    const johans = { ...hans, contactId: '12', name: 'Johans', email: 'johans@acme.com' }
    expect(bookHitsFor('Hans@ACME.com', [johans, hans])).toEqual([hans])
  })

  it('空地址什么都不命中', () => {
    expect(bookHitsFor('  ', [hans])).toEqual([])
  })
})

describe('把通讯录的信息填进收件人', () => {
  const fromHeader: AddressedPerson = { name: '', email: 'hans@acme.com', customerName: '' }

  it('通讯录里有这个人：名字、公司、联系人编号一起带上', () => {
    expect(withBookDetails(fromHeader, [hans])).toEqual({
      name: 'Hans Weber', email: 'hans@acme.com',
      contactId: '11', customerId: '5', customerName: 'ACME GmbH',
    })
  })

  it('名字以通讯录为准，原信头上写的「sales」让位', () => {
    expect(withBookDetails({ ...fromHeader, name: 'sales' }, [hans]).name).toBe('Hans Weber')
  })

  it('通讯录里没写名字：留原信头上的，公司照样带上', () => {
    const r = withBookDetails({ ...fromHeader, name: 'Hans' }, [{ ...hans, name: '' }])
    expect(r.name).toBe('Hans')
    expect(r.customerName).toBe('ACME GmbH')
  })

  it('通讯录没有这个人：原样不动', () => {
    expect(withBookDetails(fromHeader, [])).toBe(fromHeader)
  })

  it('同一地址挂在两家客户下、名字一致：名字用，公司不猜', () => {
    const twice = [hans, { ...hans, contactId: '19', customerId: '8', customerName: 'ACME Asia' }]
    const r = withBookDetails(fromHeader, twice)
    expect(r.name).toBe('Hans Weber')
    expect(r.customerName).toBe('')
    expect(r.contactId).toBeUndefined()
  })

  it('同一地址两条记录名字还不一样：名字也不猜', () => {
    const r = withBookDetails({ ...fromHeader, name: 'H.' }, [hans, { ...hans, contactId: '19', name: 'Hans W.' }])
    expect(r.name).toBe('H.')
  })

  it('保留收件人上通讯录管不着的字段', () => {
    const r = withBookDetails({ ...fromHeader, countryCode: 'DE' }, [hans])
    expect(r.countryCode).toBe('DE')
  })
})

describe('哪些格子要去问通讯录', () => {
  it('没配上通讯录的地址，去重，不带空的', () => {
    const chips = [
      { name: '', email: 'a@x.com', customerName: '' },
      { name: 'B', email: 'A@x.com', customerName: '' },
      { contactId: '3', name: 'C', email: 'c@x.com', customerName: 'X' },
      { name: '', email: '', customerName: '' },
      { name: '', email: 'd@x.com', customerName: '' },
    ]
    expect(addressesToLookUp(chips)).toEqual(['a@x.com', 'd@x.com'])
  })

  it('回复全部抄了一大串人：最多问 20 个', () => {
    const chips = Array.from({ length: 30 }, (_, i) => ({ name: '', email: `p${i}@x.com`, customerName: '' }))
    expect(addressesToLookUp(chips)).toHaveLength(20)
  })
})
