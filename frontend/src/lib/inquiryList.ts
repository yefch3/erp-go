import type { Product } from './inquiryWorkspace'
import type { TemplateField } from './inquiryTemplates'
import { productTemplateValue } from './inquiryWorkspace'

export interface InquiryListField { key:string; label:string; value:string }

const value=(product:Product,key:string)=>productTemplateValue(product,key).trim()
const withUnit=(raw:string,unit:string)=>raw && /[a-zA-Z一-鿿]/.test(raw) ? raw : raw ? `${raw}${unit}` : ''

export function materialSummary(product:Product):string {
  return [value(product,'material_standard'),value(product,'grade')].filter(Boolean).join(' ')
}

export function dimensionSummary(product:Product):string {
  const thickness=value(product,'thickness')||value(product,'wall_thickness')||value(product,'custom.thickness_mm')||value(product,'custom.wall_thickness_mm')
  const width=value(product,'width')||value(product,'custom.width_mm')
  const diameter=value(product,'diameter')||value(product,'custom.diameter_mm')
  const length=value(product,'length_or_form')
  const dimensions=[thickness,width||diameter,length].filter(Boolean)
  return dimensions.length>1?`${dimensions.join('×')}mm`:withUnit(dimensions[0]||'','mm')
}

export function productSummary(product:Product|undefined):string {
  if(!product)return '—'
  return [product.product.trim(),materialSummary(product),dimensionSummary(product)||product.specification.trim()].filter(Boolean).join(' / ')||'—'
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
