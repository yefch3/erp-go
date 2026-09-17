import type {Product,Inquiry,Quote} from './inquiryWorkspace'
import {chargeSubtotal} from './inquiryWorkspace'
export interface Calculation {formula:number;factory:string;slitting:string;shortHaul:string;inland:string;port:string;ocean:string;days:string;mtPerUnit:string}
export interface OfferLine extends Product {factoryQuoteId:string;calculation:Calculation;calculatedPrice:string;unitPrice:string;amount:string}
export interface Transport {quoteId:string;title:string;currency:string;price:string;remark:string;accepted:boolean;quantities:Record<string,string>}
export interface LogisticsAllocation {productId:string;product:string;currency:string;amount:string}
export interface CategorySelection {productId:string;category:string;quoteId:string;supplierFobUnitPrice?:string;productFreightUnitPrice?:string;cfrUnitPrice?:string;freightQuoteId?:string}
export interface OfferSelectionSnapshot extends CategorySelection {product:Product;quote:Quote}
export interface CategoryCalculation {quoteFx:string;quoteFxConfirmed:boolean;portCharge:string;inlandFreight:string;loss:string;interestRate:string;interestDays:string;note:string}
export interface OfferNegotiation extends CategorySelection {initialPrice:string;customerCounterPrice:string;proposedPrice:string;status:string;note:string}
export interface OfferBody {categoryWorkflow?:boolean;customerSelections?:OfferSelectionSnapshot[];customerLogistics?:Quote[];selectionSavedAt?:string;categoryCalculations?:Record<string,CategoryCalculation>;negotiations?:OfferNegotiation[];documentLanguage?:string;pricingSnapshot?:string;logisticsQuoteId?:string;customerId:string;customer:string;contactId:string;contact:string;currency:string;quoteFx:string;quoteFxConfirmed:boolean;delivery:string;loadingPort:string;destinationPort:string;incoterm:string;payment:string;validUntil:string;remark:string;lines:OfferLine[];transports:Transport[];categorySelections:CategorySelection[];total:string;logisticsAllocations:LogisticsAllocation[]}
export interface Offer {pricingStale?:boolean;body:OfferBody;revision:number;status:string;quotationId:string;contractId:string;canEdit:boolean;source:Inquiry}
export const emptyCalculation=():Calculation=>({formula:0,factory:'',slitting:'',shortHaul:'',inland:'',port:'',ocean:'',days:'',mtPerUnit:''})
export const factoriesFor=(quotes:Quote[],productId:string)=>quotes.filter(q=>q.kind==='PROCUREMENT'&&q.body.prices?.some(p=>p.productId===productId))
export function procurementQuoteForFormula(quotes:Quote[],productId:string,_formula:number){
 const matches=factoriesFor(quotes,productId).filter(q=>!q.historical)
 return matches.length===1?matches[0]:undefined
}
export function applyProcurementFormulaInput(line:OfferLine,quotes:Quote[],formula:number){
 const q=procurementQuoteForFormula(quotes,line.id,formula),p=q?.body.prices.find(p=>p.productId===line.id)
 if(!q||!p)return false
 line.factoryQuoteId=q.id
 line.calculation.factory=p.price||p.factoryPrice||p.fobPrice||''
 line.calculation.slitting=p.slitting||'0'
 return !!line.calculation.factory
}
export function procurementBasis(quotes:Quote[],productId:string){
 const q=procurementQuoteForFormula(quotes,productId,0),p=q?.body.prices.find(p=>p.productId===productId)
 if(!q||!p)return factoriesFor(quotes,productId).length?'请采购确定一份最终报价':'等待采购报价'
 return q.body.currency+' '+(p.price||p.factoryPrice||p.fobPrice||'—')
}
export const offerSpecification=(line:Product)=>[...new Set([...(line.specification||'').split(' · '),...Object.values(line.customFields||{})].filter(Boolean))].join(' · ')

export function offerProductTotal(lines:OfferLine[]){
  let cents=0n
  for(const line of lines){
    const amount=chargeSubtotal(line.unitPrice,line.quantity)
    if(!amount)return '待补齐'
    cents+=BigInt(amount.replace('.',''))
  }
  return (cents/100n).toString()+'.'+(cents%100n).toString().padStart(2,'0')
}
