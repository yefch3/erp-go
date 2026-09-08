import type {Product,Inquiry,Quote} from './inquiryWorkspace'
export interface Calculation {formula:number;factory:string;slitting:string;shortHaul:string;inland:string;port:string;ocean:string;days:string;mtPerUnit:string}
export interface OfferLine extends Product {factoryQuoteId:string;calculation:Calculation;calculatedPrice:string;unitPrice:string;amount:string}
export interface Transport {quoteId:string;title:string;currency:string;price:string;remark:string;accepted:boolean;quantities:Record<string,string>}
export interface OfferBody {customerId:string;customer:string;contactId:string;contact:string;currency:string;delivery:string;loadingPort:string;destinationPort:string;incoterm:string;payment:string;validUntil:string;remark:string;lines:OfferLine[];transports:Transport[];total:string}
export interface Offer {body:OfferBody;revision:number;status:string;quotationId:string;contractId:string;canEdit:boolean;source:Inquiry}
export const emptyCalculation=():Calculation=>({formula:0,factory:'',slitting:'',shortHaul:'',inland:'',port:'',ocean:'',days:'',mtPerUnit:''})
export const factoriesFor=(quotes:Quote[],productId:string)=>quotes.filter(q=>q.kind==='PROCUREMENT'&&q.body.prices?.some(p=>p.productId===productId))
