import {describe,expect,it} from 'vitest'
import {blankQuote,type Quote} from './inquiryWorkspace'
import {copyInquiryQuote,personalQuotePresentation} from './personalQuoteStatus'

describe('personal quote status',()=>{
 it.each([['WAITING','waitingForMe','warning'],['MY_QUOTED','myQuoted','success'],['UPDATED','quoteUpdated','primary'],[undefined,'waitingForMe','warning']] as const)('presents %s without inferring personal progress from department counts',(status,key,type)=>{
  expect(personalQuotePresentation({personalQuoteStatus:status})).toEqual({key,type})
 })
 it('copies content without another employee’s identity, permissions or submission state',()=>{
  const source:Quote={id:'42',kind:'LOGISTICS',version:9,body:{...blankQuote(),company:'Forwarder',charges:[{name:'Port',amount:'10',currency:'USD',quantity:'1',unit:'SHIPMENT',subtotal:'10',remark:''}]},authorId:'301',author:'Alice',submittedAt:'2026-11-15',updatedAt:'2026-11-15',updatedBy:'Alice',canEdit:false,canDelete:true,historical:false}
  const copy=copyInquiryQuote(source)
  expect(copy).toMatchObject({id:'',version:0,authorId:'',author:'',submittedAt:'',updatedAt:'',updatedBy:'',canEdit:true,canDelete:false,historical:false})
  expect(copy.body).toEqual(source.body)
  copy.body.charges[0].amount='20'
  expect(source.body.charges[0].amount).toBe('10')
 })
})
