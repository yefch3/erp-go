import {describe,it,expect} from 'vitest'
import {recognizeMailContact,resolveMailCustomer,mailContactMatches,type MailCompanyMatch} from './mailCustomerRecognition'

describe('mail signature recognition',()=>{
 it('extracts explicit details, not quoted correspondence',()=>{
  expect(recognizeMailContact('Hello\nBest regards,\nSheng Gao\nAcme Trading Ltd.\nTel: +86 12345678\nWebsite: https://acme.test\nOn yesterday Bob wrote:\nCompany: Wrong Ltd.')).toEqual({name:'Sheng Gao',companyName:'Acme Trading Ltd.',phone:'+86 12345678',website:'https://acme.test'})
 })
 it('leaves uncertain details empty and does not infer a company from a domain',()=>{
  expect(recognizeMailContact('Please quote the items.\nsg3967@columbia.edu')).toEqual({})
  expect(recognizeMailContact('Acme Ltd.\nOther Ltd.').companyName).toBeUndefined()
 })
 it('extracts explicit Chinese signature fields',()=>{
  expect(recognizeMailContact('公司名称：宁波测试有限公司\n联系人：张三\n电话：0574-12345678\n地址：宁波市测试路1号')).toEqual({companyName:'宁波测试有限公司',name:'张三',phone:'0574-12345678',address:'宁波市测试路1号'})
 })
})
const company:MailCompanyMatch={id:'1',code:'CU-1',name:'Acme',status:'ACTIVE',exactName:true,contacts:[{id:'3',name:'Alice',email:'a@acme.test',additionalEmails:['new@acme.test'],status:'ACTIVE'}]}
describe('mail company selection',()=>{
 it('chooses a single exact company for a new contact',()=>expect(resolveMailCustomer([company],'b@acme.test','Acme').company?.id).toBe('1'))
 it('recognizes an additional email without duplicating the contact',()=>expect(mailContactMatches(company.contacts![0],' NEW@ACME.TEST ')).toBe(true))
 it('requires review when the email and company disagree',()=>{
  const result=resolveMailCustomer([{...company,exactName:false}],'a@acme.test','Other')
  expect(result.conflict).toBe(true);expect(result.company).toBeNull()
 })
 it('does not pick the first owner when multiple companies use an email',()=>{
  const result=resolveMailCustomer([company,{...company,id:'2'}],'a@acme.test','Acme')
  expect(result.ambiguous).toBe(true);expect(result.company).toBeNull()
  expect(resolveMailCustomer([company,{...company,id:'2'}],'a@acme.test','Acme','2').company?.id).toBe('2')
 })
 it('does not use inactive records',()=>{
  expect(resolveMailCustomer([{...company,status:'INACTIVE'}],'a@acme.test','Acme').company).toBeNull()
  expect(mailContactMatches({...company.contacts![0],status:'INACTIVE'},'a@acme.test')).toBe(false)
 })
})
