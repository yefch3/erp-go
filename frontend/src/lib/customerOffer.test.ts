import {describe,it,expect} from 'vitest'
import {applyProcurementFormulaInput,emptyCalculation,formatCfrUnitPrice,offerProductTotal,offerSpecification,procurementBasis,type OfferLine} from './customerOffer'
import type {Quote} from './inquiryWorkspace'
import {blankProduct} from './inquiryWorkspace'
const line=():OfferLine=>({...blankProduct(),id:'p1',product:'钢管',specification:'60 mm',quantity:'3',unit:'MT',unitPrice:'12.35',calculatedPrice:'',factoryQuoteId:'',calculation:emptyCalculation(),customFields:{grade:'Q235'},amount:''})
const quote=(id:string,currency:string,factory:string,fob:string,slitting:string):Quote=>({id,kind:'PROCUREMENT',body:{currency,prices:[{productId:'p1',price:factory,factoryPrice:factory,fobPrice:fob,slitting}]}} as unknown as Quote)
describe('customer offer editing',()=>{
 it('uses the same single factory price for every trade formula',()=>{
  const q=quote('cny','CNY','700','720','0'),row=line()
  for(const formula of [1,2,3,4,5]){
   expect(applyProcurementFormulaInput(row,[q],formula)).toBe(true)
   expect(row.calculation.factory).toBe('700');expect(row.factoryQuoteId).toBe('cny')
  }
 })
 it('does not silently select a supplier when procurement has multiple quotes',()=>{
  expect(applyProcurementFormulaInput(line(),[quote('a','CNY','700','',''),quote('b','CNY','710','','')],1)).toBe(false)
 })
 it('shows only the factory quoted price',()=>{
  expect(procurementBasis([quote('a','CNY','700','720','14')],'p1')).toBe('CNY 700')
 })
 it('totals current edited values with exact decimal rounding',()=>{
  expect(offerProductTotal([line()])).toBe('37.05')
  const row=line();row.unitPrice='0.105';row.quantity='1';expect(offerProductTotal([row,row])).toBe('0.22')
  row.unitPrice='';expect(offerProductTotal([row])).toBe('待补齐')
 })
 it('includes template specification values in summaries and searches',()=>{
  expect(offerSpecification(line())).toBe('60 mm · Q235')
 })
 it('shows CFR unit prices with exactly two decimal places without changing the stored value',()=>{
  const stored='49.4118'
  expect(formatCfrUnitPrice(stored)).toBe('49.41')
  expect(formatCfrUnitPrice('110.0000')).toBe('110.00')
  expect(formatCfrUnitPrice('')).toBe('')
  expect(stored).toBe('49.4118')
 })
})
