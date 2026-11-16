import {describe,expect,it} from 'vitest'
import {blankProduct,blankQuote,type Inquiry,type Quote} from './inquiryWorkspace'
import type {Offer,OfferBody,CategorySelection} from './customerOffer'
import {automaticOfferInputKey,offerCalculationIssue,readyOfferCalculationBody} from './automaticOfferCalculation'

function fixture(){
 const supplier:Quote={id:'s1',version:1,kind:'PROCUREMENT',submittedAt:'2026-11-16',body:{...blankQuote(),currency:'USD',quoteCategory:'FOB_USD',prices:[{productId:'p1',price:'500',factoryPrice:'450',delivery:'',remark:''}]}} as Quote
 const freight:Quote={id:'f1',version:2,kind:'LOGISTICS',submittedAt:'2026-11-16',body:{...blankQuote(),validUntil:'2999-01-01',freightRates:[{productId:'p1',price:'40',usdPrice:'40',currency:'USD',unit:'MT',remark:''}]}} as Quote
 const source={id:'case1',body:{products:[{...blankProduct(),id:'p1',product:'Steel'}]},quotes:[supplier,freight]} as Inquiry
 const selection:CategorySelection={productId:'p1',quoteId:'s1',category:'FOB_USD',freightQuoteId:'f1',freightQuoteVersion:2,cfrUnitPrice:'545.4'}
 const body={customerId:'79',customer:'Test',contactId:'',contact:'',currency:'USD',quoteFx:'',quoteFxConfirmed:false,delivery:'',loadingPort:'',destinationPort:'',incoterm:'CFR',payment:'',validUntil:'',remark:'',lines:[],transports:[],total:'',logisticsAllocations:[],categorySelections:[selection],categoryCalculations:{FOB_USD:{quoteFx:'',quoteFxConfirmed:false,interestRate:'0',interestDays:'0',portCharge:'',inlandFreight:'',loss:'',note:''}}} as OfferBody
 return {source,selection,body,supplier,freight}
}
describe('automatic quote readiness',()=>{
 it('requires valid logistics and parameters, accepts zero interest, and rejects changed/expired freight',()=>{
  const {source,body,selection,freight}=fixture()
  expect(offerCalculationIssue(selection,body,source)).toBeUndefined()
  body.categoryCalculations!.FOB_USD.interestRate='';expect(offerCalculationIssue(selection,body,source)).toBe('interestRate')
  body.categoryCalculations!.FOB_USD.interestRate='-1';expect(offerCalculationIssue(selection,body,source)).toBe('interestRate')
  body.categoryCalculations!.FOB_USD.interestRate='6';freight.version=3;expect(offerCalculationIssue(selection,body,source)).toBe('freight')
  freight.version=2;freight.body.validUntil='2000-01-01';expect(offerCalculationIssue(selection,body,source)).toBe('freight')
 })
 it.each(['FOB_CNY','ALL_IN_PORT_CNY','EX_FACTORY_CNY','REPROCESSING_CNY'])('validates required inputs for %s',category=>{
  const {source,body,selection,supplier}=fixture();selection.category=category;supplier.body.quoteCategory=category;supplier.body.currency='CNY'
  const calculation={...body.categoryCalculations!.FOB_USD};body.categoryCalculations={[category]:calculation}
  expect(offerCalculationIssue(selection,body,source)).toBe('quoteFx');calculation.quoteFx='7.2'
  if(category!=='FOB_CNY'){
   expect(offerCalculationIssue(selection,body,source)).toBe('portCharge');calculation.portCharge='0'
  }
  if(['EX_FACTORY_CNY','REPROCESSING_CNY'].includes(category)){
   expect(offerCalculationIssue(selection,body,source)).toBe('inlandFreight');calculation.inlandFreight='0'
  }
  if(category==='REPROCESSING_CNY'){
   expect(offerCalculationIssue(selection,body,source)).toBe('loss');calculation.loss='0'
  }
  expect(offerCalculationIssue(selection,body,source)).toBeUndefined()
 })
 it('direct CFR needs no logistics, but still requires interest parameters',()=>{
  const {source,body,selection,supplier}=fixture();selection.category='DIRECT_CFR_USD';supplier.body.quoteCategory=selection.category;selection.freightQuoteId=''
  body.categoryCalculations={DIRECT_CFR_USD:{...body.categoryCalculations!.FOB_USD}}
  expect(offerCalculationIssue(selection,body,source)).toBeUndefined()
 })
 it('calculates ready rows without incomplete rows blocking them and preserves the original body',()=>{
  const {source,body,selection}=fixture();const missing={...selection,freightQuoteId:undefined,quoteId:'missing'};body.categorySelections.push(missing)
  const ready=readyOfferCalculationBody(body,source)
  expect(ready.categorySelections).toEqual([selection]);expect(body.categorySelections).toHaveLength(2)
 })
 it('ignores calculated outputs but invalidates on inputs, source versions and logistics changes',()=>{
  const {source,body,selection,freight}=fixture();const offer={source,body} as Offer;const original=automaticOfferInputKey(offer)
  selection.cfrUnitPrice='999';selection.productFreightUnitPrice='888';expect(automaticOfferInputKey(offer)).toBe(original)
  body.categoryCalculations!.FOB_USD.interestDays='60';expect(automaticOfferInputKey(offer)).not.toBe(original)
  const changed=automaticOfferInputKey(offer);freight.version++;expect(automaticOfferInputKey(offer)).not.toBe(changed)
 })
})
