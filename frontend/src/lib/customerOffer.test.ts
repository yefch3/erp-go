import {describe,it,expect} from 'vitest'
import {emptyCalculation,selectOnlyFactory,offerProductTotal,offerSpecification,type OfferLine} from './customerOffer'
import type {Quote} from './inquiryWorkspace'
import {blankProduct} from './inquiryWorkspace'
const line=():OfferLine=>({...blankProduct(),id:'p1',product:'钢管',specification:'60 mm',quantity:'3',unit:'MT',unitPrice:'12.35',calculatedPrice:'',factoryQuoteId:'',calculation:emptyCalculation(),customFields:{grade:'Q235'},amount:''})
const quote=(id:string):Quote=>({id,kind:'PROCUREMENT',body:{prices:[{productId:'p1',price:'10'}]}} as unknown as Quote)
describe('customer offer editing',()=>{
 it('selects a sole source without overwriting a negotiated price',()=>{
  const row=line();selectOnlyFactory(row,[quote('q1')])
  expect(row.factoryQuoteId).toBe('q1');expect(row.calculation.factory).toBe('10');expect(row.calculatedPrice).toBe('');expect(row.unitPrice).toBe('12.35')
 })
 it('does not guess between factories and uses the sole procurement result',()=>{
  const row=line();row.unitPrice='';selectOnlyFactory(row,[quote('q1'),quote('q2')])
  expect(row.factoryQuoteId).toBe('');selectOnlyFactory(row,[quote('q1')]);expect(row.factoryQuoteId).toBe('q1');expect(row.calculation.factory).toBe('10');expect(row.unitPrice).toBe('')
 })
 it('retains the selected factory when sources refresh',()=>{
  const row=line();row.factoryQuoteId='q2';selectOnlyFactory(row,[quote('q1'),quote('q2')]);expect(row.factoryQuoteId).toBe('q2')
 })
 it('totals current edited values with exact decimal rounding',()=>{
  expect(offerProductTotal([line()])).toBe('37.05')
  const row=line();row.unitPrice='0.105';row.quantity='1';expect(offerProductTotal([row,row])).toBe('0.22')
  row.unitPrice='';expect(offerProductTotal([row])).toBe('待补齐')
 })
 it('includes template specification values in summaries and searches',()=>{
  expect(offerSpecification(line())).toBe('60 mm · Q235')
 })
})
