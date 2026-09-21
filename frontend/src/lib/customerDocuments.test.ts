import { describe, expect, it } from 'vitest'
import { documentExpiry } from './customerDocuments'
describe('customer document validity',()=>{
 const now=new Date('2026-09-20T23:59:00Z')
 it('handles expiry boundaries and permanent files',()=>{
  expect(documentExpiry('',30,now).label).toBe('长期有效')
  expect(documentExpiry('2026-09-19',30,now)).toEqual({label:'已过期 1 天',type:'danger'})
  expect(documentExpiry('2026-09-20',30,now).label).toBe('今天到期')
  expect(documentExpiry('2026-09-21',0,now)).toEqual({label:'剩余 1 天',type:'success'})
  expect(documentExpiry('2026-09-21',1,now).type).toBe('warning')
 })
 it('uses calendar days across a daylight saving transition',()=>{
  expect(documentExpiry('2026-11-02',30,new Date('2026-11-01T05:00:00Z')).label).toBe('剩余 1 天')
 })
})
