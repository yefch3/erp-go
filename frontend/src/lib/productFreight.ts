import type {Product,Quote} from './inquiryWorkspace'
import type {CategorySelection} from './customerOffer'
import {formatCfrUnitPrice} from './customerOffer'

export const freightProductKey=(product:Pick<Product,'product'|'id'>)=>product.product.trim().toLocaleLowerCase()||product.id

export function productFreightPrices(products:Product[],quote?:Quote){
 if(!quote)return []
 return products.flatMap(product=>{
  if(!freightCovers(quote,product.id))return []
  const rate=quote.body.freightRates.find(r=>r.productId===product.id)!
  return [{product,price:rate.usdPrice!,unit:rate.unit||product.unit}]
 })
}

export function productFreightPriceSummary(products:Product[],quote:Quote){
 const units=new Map<string,number[]>()
 for(const rate of productFreightPrices(products,quote)){
  const prices=units.get(rate.unit)||[]
  prices.push(Number(rate.price));units.set(rate.unit,prices)
 }
 return [...units].map(([unit,prices])=>{
  const min=Math.min(...prices),max=Math.max(...prices)
  const amount=min===max?formatCfrUnitPrice(String(min)):`${formatCfrUnitPrice(String(min))}–${formatCfrUnitPrice(String(max))}`
  return `USD ${amount} / ${unit}`
 }).join(' · ')
}

export function freightCovers(quote:Quote,productId:string,today=new Date().toISOString().slice(0,10)){
 return quote.kind==='LOGISTICS'&&!quote.historical&&!!quote.submittedAt&&
  (!quote.body.validUntil||quote.body.validUntil>=today)&&
  !!quote.body.freightRates?.some(rate=>rate.productId===productId&&!!rate.usdPrice?.trim()&&Number.isFinite(Number(rate.usdPrice))&&Number(rate.usdPrice)>=0)
}

export function commonProductFreight(products:Product[],selections:CategorySelection[],quotes:Quote[]){
 const ids=new Set(products.map(p=>p.id))
 const relevant=selections.filter(s=>ids.has(s.productId)&&s.category!=='DIRECT_CFR_USD')
 const first=relevant[0]
 if(!first?.freightQuoteId||!relevant.every(s=>s.freightQuoteId===first.freightQuoteId&&s.freightQuoteVersion===first.freightQuoteVersion))return undefined
 const quote=quotes.find(q=>q.id===first.freightQuoteId&&q.version===first.freightQuoteVersion)
 return quote&&products.every(p=>freightCovers(quote,p.id))?quote.id:undefined
}

export function applyProductFreight(products:Product[],selections:CategorySelection[],quote?:Quote){
 if(quote&&!products.every(p=>freightCovers(quote,p.id)))return false
 const ids=new Set(products.map(p=>p.id))
 for(const selection of selections){
  if(!ids.has(selection.productId)||selection.category==='DIRECT_CFR_USD')continue
  selection.freightQuoteId=quote?.id
  selection.freightQuoteVersion=quote?.version
  selection.productFreightUnitPrice=''
  selection.cfrUnitPrice=''
 }
 return true
}
