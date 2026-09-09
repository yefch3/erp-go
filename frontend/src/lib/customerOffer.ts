import type {Product,Inquiry,Quote} from './inquiryWorkspace'
import {chargeSubtotal} from './inquiryWorkspace'
export interface Calculation {formula:number;factory:string;slitting:string;shortHaul:string;inland:string;port:string;ocean:string;days:string;mtPerUnit:string}
export interface OfferLine extends Product {factoryQuoteId:string;calculation:Calculation;calculatedPrice:string;unitPrice:string;amount:string}
export interface Transport {quoteId:string;title:string;currency:string;price:string;remark:string;accepted:boolean;quantities:Record<string,string>}
export interface OfferBody {customerId:string;customer:string;contactId:string;contact:string;currency:string;delivery:string;loadingPort:string;destinationPort:string;incoterm:string;payment:string;validUntil:string;remark:string;lines:OfferLine[];transports:Transport[];total:string}
export interface Offer {body:OfferBody;revision:number;status:string;quotationId:string;contractId:string;canEdit:boolean;source:Inquiry}
export const emptyCalculation=():Calculation=>({formula:0,factory:'',slitting:'',shortHaul:'',inland:'',port:'',ocean:'',days:'',mtPerUnit:''})
export const factoriesFor=(quotes:Quote[],productId:string)=>quotes.filter(q=>q.kind==='PROCUREMENT'&&q.body.prices?.some(p=>p.productId===productId))
export const offerSpecification=(line:Product)=>[...new Set([...(line.specification||'').split(' · '),...Object.values(line.customFields||{})].filter(Boolean))].join(' · ')
export function selectOnlyFactory(line:OfferLine,quotes:Quote[]){
  const matches=factoriesFor(quotes,line.id)
  if(!line.factoryQuoteId&&matches.length===1)line.factoryQuoteId=matches[0].id
  const selected=matches.find(q=>q.id===line.factoryQuoteId)
  if(selected){
   const price=selected.body.prices.find(p=>p.productId===line.id)
   line.calculation.factory=price?.price||''
  }
}
export function offerProductTotal(lines:OfferLine[]){
  let cents=0n
  for(const line of lines){
    const amount=chargeSubtotal(line.unitPrice,line.quantity)
    if(!amount)return '待补齐'
    cents+=BigInt(amount.replace('.',''))
  }
  return (cents/100n).toString()+'.'+(cents%100n).toString().padStart(2,'0')
}
