import { describe, expect, it } from 'vitest'
import { optionalSourcingContact } from './sourcingTransfer'

describe('optionalSourcingContact', () => {
  it('联系人留空时不向 int64 字段发送空字符串', () => {
    expect(optionalSourcingContact('')).toEqual({})
    expect(optionalSourcingContact('0')).toEqual({})
    expect(optionalSourcingContact(undefined)).toEqual({})
    expect(JSON.stringify({ customerId: '4', ...optionalSourcingContact('') }))
      .toBe('{"customerId":"4"}')
  })

  it('选中联系人时保留字符串 id，不经过 Number', () => {
    expect(optionalSourcingContact(' 9007199254740993 ')).toEqual({
      contactId: '9007199254740993',
    })
  })
})
