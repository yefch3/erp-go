import type { Product } from './inquiryWorkspace'
import type { TemplateField } from './inquiryTemplates'
import { productTemplateValue } from './inquiryWorkspace'

export interface InquiryListField { key:string; label:string; value:string }
export interface InquirySpecificationGroup { key:string; products:Product[]; representative:Product }
export interface InquiryProductGroup { key:string; name:string; products:Product[]; specifications:InquirySpecificationGroup[] }

const value=(product:Product,key:string)=>productTemplateValue(product,key).trim()
const withUnit=(raw:string,unit:string)=>raw && /[a-zA-Z一-鿿]/.test(raw) ? raw : raw ? `${raw}${unit}` : ''

export function materialSummary(product:Product):string {
  return [value(product,'material_standard'),value(product,'grade')].filter(Boolean).join(' ')
}

export function dimensionFields(product:Product):InquiryListField[] {
  const candidates:[string,string][]=[
    ['thickness',value(product,'thickness')||value(product,'custom.thickness_mm')],
    ['wall_thickness',value(product,'wall_thickness')||value(product,'custom.wall_thickness_mm')],
    ['width',value(product,'width')||value(product,'custom.width_mm')],
    ['height',value(product,'height')||value(product,'custom.height_mm')],
    ['diameter',value(product,'diameter')||value(product,'custom.diameter_mm')],
    ['length_or_form',value(product,'length_or_form')],
  ]
  return candidates.filter(([,fieldValue])=>fieldValue).map(([key,fieldValue])=>({key,label:key,value:withUnit(fieldValue,' mm')}))
}

export function dimensionSummary(product:Product):string {
  return dimensionFields(product).map(field=>field.value).join(' × ')
}

export function productSummary(product:Product|undefined):string {
  if(!product)return '—'
  return [product.product.trim(),materialSummary(product),dimensionSummary(product)||product.specification.trim()].filter(Boolean).join(' / ')||'—'
}

function stableSpecificationKey(product:Product):string {
  const custom=Object.fromEntries(Object.entries(product.customFields||{}).sort(([a],[b])=>a.localeCompare(b)))
  return JSON.stringify({specification:product.specification,unit:product.unit,delivery:product.delivery,weight:product.weight,volume:product.volume,packaging:product.packaging,packageQuantity:product.packageQuantity||'',remark:product.remark,custom})
}

export function groupInquiryProducts(products:Product[]):InquiryProductGroup[] {
  const groups=new Map<string,InquiryProductGroup>()
  for(const product of products){
    const name=product.product.trim()||'—',key=name.toLocaleLowerCase()
    let group=groups.get(key)
    if(!group){group={key,name,products:[],specifications:[]};groups.set(key,group)}
    group.products.push(product)
  }
  for(const group of groups.values()){
    const specifications=new Map<string,InquirySpecificationGroup>()
    for(const product of group.products){
      const key=stableSpecificationKey(product)
      let specification=specifications.get(key)
      if(!specification){specification={key,products:[],representative:product};specifications.set(key,specification)}
      specification.products.push(product)
    }
    group.specifications=[...specifications.values()]
  }
  return [...groups.values()]
}

export function groupedQuantity(products:Product[]):string {
  const totals=new Map<string,number>(),unstructured:string[]=[]
  for(const product of products){
    const quantity=Number(product.quantity.trim()),unit=product.unit.trim()
    if(Number.isFinite(quantity))totals.set(unit,(totals.get(unit)||0)+quantity)
    else if(product.quantity.trim())unstructured.push(`${product.quantity.trim()}${unit?` ${unit}`:''}`)
  }
  return [...totals.entries()].map(([unit,total])=>`${total.toFixed(2)}${unit?` ${unit}`:''}`).concat(unstructured).join('；')||'—'
}

export function specificationAuxiliaryFields(product:Product,fields:TemplateField[]=[]):InquiryListField[] {
  const excluded=new Set(['material_standard','specification','quantity','quantity_unit','unit','thickness','wall_thickness','width','height','diameter','length_or_form','custom.thickness_mm','custom.wall_thickness_mm','custom.width_mm','custom.height_mm','custom.diameter_mm'])
  return inquiryListFields(product,fields).filter(field=>excluded.has(field.key)===false)
}

export function inquiryListFields(product:Product,fields:TemplateField[]=[]):InquiryListField[] {
  const material=materialSummary(product)
  const specification=dimensionSummary(product)||product.specification.trim()
  const result:InquiryListField[]=[
    {key:'material_standard',label:'material_standard',value:material||'—'},
    {key:'specification',label:'specification',value:specification||'—'},
    {key:'quantity',label:'quantity',value:product.quantity.trim()||'—'},
    {key:'quantity_unit',label:'unit',value:product.unit.trim()||'—'},
  ]
  const represented=new Set(['product','material_standard','grade','specification','quantity','quantity_unit','unit','unit_price','total_price'])
  const added=new Set<string>()
  for(const field of [...fields].sort((a,b)=>a.sortOrder-b.sortOrder)){
    if(represented.has(field.fieldKey))continue
    const fieldValue=value(product,field.fieldKey)
    if(fieldValue){result.push({key:field.fieldKey,label:field.displayName||field.fieldKey,value:fieldValue});added.add(field.fieldKey)}
  }
  for(const key of ['delivery','weight','volume','packaging','package_quantity','remarks']){
    const fieldValue=value(product,key)
    if(!represented.has(key)&&!added.has(key)&&fieldValue){result.push({key,label:key,value:fieldValue});added.add(key)}
  }
  for(const [key,fieldValue] of Object.entries(product.customFields||{})){
    if(!represented.has(key)&&!added.has(key)&&fieldValue.trim())result.push({key,label:key,value:fieldValue.trim()})
  }
  return result
}
