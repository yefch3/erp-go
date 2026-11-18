import type {Product,Quote,Price} from './inquiryWorkspace'
import type {CategorySelection} from './customerOffer'
import {productTemplateValue} from './inquiryWorkspace'
import type {TemplateField} from './inquiryTemplates'

export type Candidate={product:Product;quote:Quote;price:Price;category:string}
export type SpecificationCandidateGroup={key:string;product:Product;rows:Candidate[]}
export type ProductCandidateGroup={key:string;productName:string;category:string;specs:SpecificationCandidateGroup[];rows:Candidate[]}
export type ComparisonSpecificationRow=SpecificationCandidateGroup&{category:string;productGroup:ProductCandidateGroup}

export function comparisonSpecificationRows(groups:ProductCandidateGroup[]):ComparisonSpecificationRow[]{
 return groups.flatMap(productGroup=>productGroup.specs.map(spec=>({...spec,category:productGroup.category,productGroup})))
}

// Replace one specification/category only; hidden or other-category choices survive.
export function replaceSpecificationSelection(current:CategorySelection[],productId:string,category:string,quoteId:string|undefined,allowedQuoteIds:string[]):CategorySelection[]{
 const allowed=new Set(allowedQuoteIds)
 const selected=quoteId&&allowed.has(quoteId)?[quoteId]:[]
 const previous=current.filter(s=>s.productId===productId&&s.category===category)
 return [
  ...current.filter(s=>s.productId!==productId||s.category!==category),
  ...selected.map(quoteId=>previous.find(s=>s.quoteId===quoteId)||{productId,category,quoteId}),
 ]
}

export function comparisonProductFields(product:Product,template:TemplateField[],label:(key:string)=>string){
 const hidden=new Set(['product','quantity','quantity_unit'])
 const result=template.slice().sort((a,b)=>a.sortOrder-b.sortOrder).flatMap(field=>{
  const value=productTemplateValue(product,field.fieldKey).trim()
  return !hidden.has(field.fieldKey)&&value?[{key:field.fieldKey,label:field.displayName,value}]:[]
 })
 const seen=new Set([...hidden,...result.map(field=>field.key)])
 const supplemental:[string,string][]=[['specification',product.specification],['delivery',product.delivery],['weight',product.weight],['volume',product.volume],['packaging',product.packaging],['package_quantity',product.packageQuantity||''],['remarks',product.remark],...Object.entries(product.customFields||{})]
 for(const [key,raw] of supplemental){const value=(raw||'').trim();if(value&&!seen.has(key)){seen.add(key);result.push({key,label:label(key),value})}}
 return result
}

// Presentation only: retain the raw name, then append distinct source fields.
// Equal values in different fields (such as weight and volume) are not duplicates.
export function comparisonProductDescription(product:Product,fields:{key:string;label:string;value:string}[]){
 const name=product.product.trim(),parts=name?[product.product]:[],seen=new Set<string>()
 for(const field of fields){
  const value=field.value.trim(),key=JSON.stringify([field.key,value])
  if(!value||seen.has(key)||(field.key==='specification'&&value===name))continue
  seen.add(key);parts.push(`${field.label}: ${field.value}`)
 }
 return parts.join('\n')||'—'
}

export function comparisonHeadingName(name:string){
 const trimmed=name.trim()
 return [...trimmed].length<=40&&!/[\r\n]/.test(trimmed)?trimmed:''
}

export function hasMultipleSpecificationSuppliers(selections:CategorySelection[]):boolean{
 const seen=new Set<string>()
 return selections.some(s=>{const key=JSON.stringify([s.productId,s.category]);if(seen.has(key))return true;seen.add(key);return false})
}
