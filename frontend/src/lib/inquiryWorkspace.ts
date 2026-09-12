import type { TemplateField } from './inquiryTemplates'

export interface InquiryTemplateSnapshot {
 id:string
 templateCode:string
 version:number
 name:string
 fields:TemplateField[]
}
export interface Product { id:string; product:string; specification:string; quantity:string; unit:string; delivery:string; weight:string; volume:string; packaging:string; packageQuantity?:string; remark:string; customFields:Record<string,string> }
export interface Attachment {key:string;name:string}
export interface InquiryBody { title?:string;customerId?:string;customer:string;contactId?:string;contact:string;delivery:string;loadingPort:string;destinationPort:string;incoterm:string;remark:string;template?:InquiryTemplateSnapshot;products:Product[];attachments:Attachment[] }
export interface Price {productId:string;price:string;delivery:string;remark:string}
export interface Charge {name:string;amount:string;currency:string;unit:string;quantity:string;subtotal:string;remark:string}
export interface QuoteBody {company:string;currency:string;delivery:string;validUntil:string;paymentTerms:string;incoterm:string;remark:string;prices:Price[];carrier:string;route:string;vessel:string;voyage:string;departure:string;arrival:string;transitDays:string;loadingPort:string;destinationPort:string;cargoIds:string[];charges:Charge[];totals:Record<string,string>;attachments:Attachment[]}
export interface Quote {historical?:boolean;id:string;kind:string;version:number;body:QuoteBody;authorId:string;author:string;submittedAt:string;updatedBy:string;updatedAt:string;canEdit:boolean}
export interface Inquiry {id:string;number:string;ownerId:string;owner:string;state:string;revision:number;submittedAt:string;body:InquiryBody;quotes:Quote[];procurementCount:number;logisticsCount:number;canEdit:boolean;sourceMailId:string;legacy:boolean}
export interface Result {items:Inquiry[];total:number;item?:Inquiry;attachment?:Attachment;url?:string}
export const blankProduct=():Product=>({id:'',product:'',specification:'',quantity:'',unit:'',delivery:'',weight:'',volume:'',packaging:'',remark:'',customFields:{}})
export const blankBody=():InquiryBody=>({title:'',customerId:'',customer:'',contactId:'',contact:'',delivery:'',loadingPort:'',destinationPort:'',incoterm:'',remark:'',products:[blankProduct()],attachments:[]})
export const blankQuote=():QuoteBody=>({company:'',currency:'USD',delivery:'',validUntil:'',paymentTerms:'',incoterm:'',remark:'',prices:[],carrier:'',route:'',vessel:'',voyage:'',departure:'',arrival:'',transitDays:'',loadingPort:'',destinationPort:'',cargoIds:[],charges:[],totals:{},attachments:[]})
export function pastePrices(text:string,products:Product[],prices:Price[],startId:string):Price[]{
 const start=products.findIndex(p=>p.id===startId);if(start<0)return prices
 const out=prices.map(p=>({...p}));text.trimEnd().split(/\r?\n/).forEach((line,i)=>{const p=products[start+i];if(!p)return;const [price,delivery,remark]=line.split('\t');if(!price?.trim())return;let row=out.find(r=>r.productId===p.id);if(!row){row={productId:p.id,price:'',delivery:'',remark:''};out.push(row)}row.price=price.trim();if(delivery!==undefined)row.delivery=delivery.trim();if(remark!==undefined)row.remark=remark})
 return out
}

// Display-only decimal arithmetic. The server recomputes every subtotal and
// currency total; BigInt prevents misleading floating-point totals while typing.
export function chargeSubtotal(amount:string,quantity:string):string {
 const parse=(v:string)=>{const m=/^(\d+)(?:\.(\d+))?$/.exec(v.trim());return m?{n:BigInt(m[1]+(m[2]||'')),scale:(m[2]||'').length}:null}
 const a=parse(amount),q=parse(quantity);if(!a||!q)return ''
 const den=10n**BigInt(a.scale+q.scale),scaled=a.n*q.n*100n
 const cents=scaled/den+(scaled%den*2n>=den?1n:0n)
 return `${cents/100n}.${String(cents%100n).padStart(2,'0')}`
}
export function chargeTotals(charges:Charge[]):Record<string,string>{
 const totals:Record<string,bigint>={}
 for(const c of charges){const sub=chargeSubtotal(c.amount,c.quantity),currency=c.currency.trim().toUpperCase();if(!sub||!currency)continue;totals[currency]=(totals[currency]||0n)+BigInt(sub.replace('.',''))}
 return Object.fromEntries(Object.entries(totals).map(([k,n])=>[k,`${n/100n}.${String(n%100n).padStart(2,'0')}`]))
}

const productPropertyByTemplateKey:Record<string,keyof Product>={
 product:'product',quantity:'quantity',quantity_unit:'unit',delivery:'delivery',packaging:'packaging',remarks:'remark',
 specification:'specification',weight:'weight',volume:'volume',package_quantity:'packageQuantity',
}

export function productTemplateValue(product:Product,key:string):string{
 const property=productPropertyByTemplateKey[key]
 if(property)return String(product[property]??'')
 return product.customFields?.[key]??''
}

export function setProductTemplateValue(product:Product,key:string,value:string):void{
 const property=productPropertyByTemplateKey[key]
 if(property){(product as unknown as Record<string,string>)[property]=value;return}
 product.customFields??={}
 product.customFields[key]=value
}

export function applyTemplateDefaults(product:Product,fields:TemplateField[]):void{
 product.customFields??={}
 for(const field of fields){
  if(!productTemplateValue(product,field.fieldKey)&&field.defaultValue)setProductTemplateValue(product,field.fieldKey,field.defaultValue)
 }
}

// Source quantities are grouped by their stated unit; mixed units are never
// silently combined. Missing/unstructured values remain visible in the rows.
export function productTotal(products:Product[],field:'quantity'|'weight'|'volume'|'packageQuantity',unitField?:'unit'):string {
 const groups:Record<string,{n:bigint;scale:number}>={};let missing=0
 for(const p of products){const m=/^(\d+)(?:\.(\d+))?\s*(.*)$/.exec((p[field]||'').trim());if(!m){missing++;continue}
 const unit=unitField?p[unitField]:m[3],scale=(m[2]||'').length,n=BigInt(m[1]+(m[2]||'')),old=groups[unit]||{n:0n,scale:0},common=Math.max(old.scale,scale)
 groups[unit]={n:old.n*10n**BigInt(common-old.scale)+n*10n**BigInt(common-scale),scale:common}}
 const text=Object.entries(groups).map(([unit,{n,scale}])=>{const digits=String(n).padStart(scale+1,'0');return `${scale?`${digits.slice(0,-scale)}.${digits.slice(-scale)}`:digits}${unit?' '+unit:''}`}).join('；')
 return text?text+(missing?'（部分未填写）':''):'—'
}
