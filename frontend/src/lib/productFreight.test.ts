import {describe,it,expect} from 'vitest'
import {blankProduct,type Quote} from './inquiryWorkspace'
import type {CategorySelection} from './customerOffer'
import {applyProductFreight,commonProductFreight,freightCovers,freightProductKey,productFreightPrices,productFreightPriceSummary} from './productFreight'

const products=[{...blankProduct(),id:'p1',product:'钢卷',specification:'3mm'},{...blankProduct(),id:'p2',product:'钢卷',specification:'4mm'}]
const quote=(id='freight',version=2):Quote=>({id,version,kind:'LOGISTICS',submittedAt:'2026-11-16',body:{validUntil:'2999-01-01',freightRates:[{productId:'p1',usdPrice:'38'},{productId:'p2',usdPrice:'40'}]}} as Quote)
const selection=(productId:string,quoteId:string,category='FOB_USD'):CategorySelection=>({productId,quoteId,category,cfrUnitPrice:'550',productFreightUnitPrice:'30'})

describe('product-wide logistics selection',()=>{
 it('shows all specification prices and their range independently of supplier selection',()=>{
  const q=quote()
  q.body.freightRates.forEach(r=>{r.unit='MT'})
  expect(productFreightPriceSummary(products,q)).toBe('USD 38.00–40.00 / MT')
  expect(productFreightPrices(products,q).map(r=>r.price)).toEqual(['38','40'])
  expect(productFreightPrices(products,undefined)).toEqual([])
 })
 it('shows equal prices once and keeps different pricing units separate',()=>{
  const q=quote()
  q.body.freightRates.forEach(r=>{r.unit='MT';r.usdPrice='0'})
  expect(productFreightPriceSummary(products,q)).toBe('USD 0.00 / MT')
  q.body.freightRates[1].unit='CBM';q.body.freightRates[1].usdPrice='10'
  expect(productFreightPriceSummary(products,q)).toBe('USD 0.00 / MT · USD 10.00 / CBM')
  q.body.freightRates[0].usdPrice=''
  expect(productFreightPrices(products,q).map(r=>r.product.id)).toEqual(['p2'])
 })
 it('applies one logistics plan across specifications, suppliers and quote categories without touching another product or direct CFR',()=>{
  const rows=[selection('p1','supplierA'),selection('p2','supplierB','FOB_CNY'),selection('other','supplierA'),selection('p1','direct','DIRECT_CFR_USD')]
  expect(applyProductFreight(products,rows,quote())).toBe(true)
  expect(rows.slice(0,2).map(r=>[r.freightQuoteId,r.freightQuoteVersion,r.cfrUnitPrice,r.productFreightUnitPrice])).toEqual([['freight',2,'',''],['freight',2,'','']])
  expect(rows[2].cfrUnitPrice).toBe('550');expect(rows[3].freightQuoteId).toBeUndefined()
  expect(quote().body.freightRates.map(r=>r.usdPrice)).toEqual(['38','40'])
 })
 it('restores a common saved plan for newly selected suppliers and detects legacy mixed plans',()=>{
  const rows=[selection('p1','a'),selection('p2','b')]
  applyProductFreight(products,rows,quote())
  expect(commonProductFreight(products,rows,[quote()])).toBe('freight')
  const next=selection('p2','new supplier');applyProductFreight(products,[next],quote())
  expect(next.freightQuoteId).toBe('freight')
  rows[1].freightQuoteId='other'
  expect(commonProductFreight(products,rows,[quote()])).toBeUndefined()
 })
 it('rejects incomplete or changed quotes and clears a product plan without leaving calculated amounts',()=>{
  const rows=[selection('p1','a'),selection('p2','b')],partial=quote()
  partial.body.freightRates.pop()
  expect(applyProductFreight(products,rows,partial)).toBe(false)
  expect(rows[0].cfrUnitPrice).toBe('550')
  applyProductFreight(products,rows,quote())
  expect(commonProductFreight(products,rows,[quote('freight',3)])).toBeUndefined()
  applyProductFreight(products,rows)
  expect(rows.every(r=>!r.freightQuoteId&&!r.freightQuoteVersion&&!r.cfrUnitPrice&&!r.productFreightUnitPrice)).toBe(true)
 })
 it('requires current submitted quotes with valid converted rates, including zero freight',()=>{
  const q=quote()
  q.body.validUntil='2000-01-01';expect(freightCovers(q,'p1')).toBe(false)
  q.body.validUntil='2999-01-01';q.body.freightRates[0].usdPrice='';expect(freightCovers(q,'p1')).toBe(false)
  q.body.freightRates[0].usdPrice='0';expect(freightCovers(q,'p1')).toBe(true)
  q.historical=true;expect(freightCovers(q,'p1')).toBe(false)
  expect(freightProductKey({...products[0],product:' 钢卷 '})).toBe(freightProductKey(products[1]))
 })
})
