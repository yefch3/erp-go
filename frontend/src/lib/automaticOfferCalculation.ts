import type {Offer,OfferBody,CategorySelection} from './customerOffer'
import type {Inquiry} from './inquiryWorkspace'
import {freightCovers} from './productFreight'

const numeric=(value?:string,positive=false)=>!!value?.trim()&&Number.isFinite(Number(value))&&(positive?Number(value)>0:Number(value)>=0)
export function offerCalculationIssue(selection:CategorySelection,body:OfferBody,source:Inquiry):string|undefined{
 const quote=source.quotes.find(q=>q.id===selection.quoteId&&q.kind==='PROCUREMENT'&&!q.historical&&q.submittedAt&&q.body.quoteCategory===selection.category)
 const price=quote?.body.prices?.find(p=>p.productId===selection.productId)
 if(!quote||!price)return 'supplier'
 const usd=['FOB_USD','DIRECT_CFR_USD'].includes(selection.category)
 if(quote.body.currency!==(usd?'USD':'CNY')||!numeric(price.price||price.fobPrice||price.factoryPrice))return 'supplierPrice'
 if(selection.category==='REPROCESSING_CNY'&&!numeric(price.factoryPrice))return 'factoryPrice'
 if(selection.category!=='DIRECT_CFR_USD'){
  const freight=source.quotes.find(q=>q.id===selection.freightQuoteId&&q.version===selection.freightQuoteVersion)
  if(!freight||!freightCovers(freight,selection.productId))return 'freight'
 }
 const calculation=body.categoryCalculations?.[selection.category]
 const required=['interestRate','interestDays']
 if(!usd)required.push('quoteFx')
 if(['ALL_IN_PORT_CNY','EX_FACTORY_CNY','REPROCESSING_CNY'].includes(selection.category))required.push('portCharge')
 if(['EX_FACTORY_CNY','REPROCESSING_CNY'].includes(selection.category))required.push('inlandFreight')
 if(selection.category==='REPROCESSING_CNY')required.push('loss')
 for(const field of ['quoteFx','portCharge','inlandFreight','loss','interestRate','interestDays'] as const){
  const value=calculation?.[field]
  if((required.includes(field)||value?.trim())&&!numeric(value,field==='quoteFx'))return field
 }
}

export function automaticOfferInputKey(offer:Offer){
 return JSON.stringify({source:offer.source,selections:offer.body.categorySelections.map(({productId,category,quoteId,freightQuoteId,freightQuoteVersion})=>({productId,category,quoteId,freightQuoteId,freightQuoteVersion})),calculations:offer.body.categoryCalculations})
}

export function readyOfferCalculationBody(body:OfferBody,source:Inquiry):OfferBody{
 const categorySelections=body.categorySelections.filter(s=>!offerCalculationIssue(s,body,source))
 const categories=new Set(categorySelections.map(s=>s.category))
 return {...body,categoryWorkflow:true,categorySelections,categoryCalculations:Object.fromEntries(Object.entries(body.categoryCalculations||{}).filter(([category])=>categories.has(category)))}
}
