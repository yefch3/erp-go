import type { DirectSheet } from './attachmentExcel'
import type { InquiryTemplate, TemplateField } from './inquiryTemplates'
import { applyTemplateDefaults, blankProduct, setProductTemplateValue, type Product } from './inquiryWorkspace'

function normalized(value:string):string{return value.replace(/^\uFEFF/,'').trim().toLocaleLowerCase()}

function templateAccepts(headers:string[],template:InquiryTemplate):boolean{
 const expected=new Set((template.fields||[]).map(field=>normalized(field.displayName)))
 const seen=new Set<string>()
 for(const header of headers){const key=normalized(header);if(!key||seen.has(key)||!expected.has(key))return false;seen.add(key)}
 return true
}

export function selectImportTemplate(headers:string[],templates:InquiryTemplate[],requestedId=''):InquiryTemplate{
 const active=templates.filter(template=>template.status==='ACTIVE'&&(template.fields?.length||0)>0)
 if(requestedId){const selected=active.find(template=>String(template.id)===requestedId);if(!selected)throw new Error('所选询盘格式已停用或不存在');if(!templateAccepts(headers,selected))throw new Error('文件中存在所选格式没有定义的表头，无法导入');return selected}
 const knownHeaders=new Set(active.flatMap(template=>(template.fields||[]).map(field=>normalized(field.displayName))))
 const unknown=headers.filter(header=>!knownHeaders.has(normalized(header)))
 if(unknown.length)throw new Error(`文件中存在生效格式没有定义的表头：${unknown.join('、')}`)
 const matches=active.filter(template=>templateAccepts(headers,template))
 if(matches.length===1)return matches[0]
 const defaults=matches.filter(template=>template.isDefault)
 if(defaults.length===1)return defaults[0]
 if(matches.length>1)throw new Error('文件表头同时匹配多个询盘格式，请明确选择格式')
 throw new Error('无法根据文件表头识别询盘格式，请选择正确格式后重试')
}

export function productsFromImportedSheet(sheet:DirectSheet,template:InquiryTemplate):Product[]{
 if(sheet.totalRows>10000)throw new Error('单次最多导入 10000 条产品明细')
 const columns=new Map<string,number>()
 sheet.columns.forEach((header,index)=>columns.set(normalized(header),index))
 const fields=(template.fields||[]).slice().sort((a,b)=>a.sortOrder-b.sortOrder)
 const products:Product[]=[]
 for(const cells of sheet.rows){
  if(!cells.some(cell=>cell.trim()!==''))continue
  const product=blankProduct();applyTemplateDefaults(product,fields)
  for(const field of fields){const index=columns.get(normalized(field.displayName));if(index!==undefined)setProductTemplateValue(product,field.fieldKey,(cells[index]||'').trim()||field.defaultValue)}
  products.push(product)
 }
 if(!products.length)throw new Error('标准询盘文件没有可导入的产品明细')
 return products
}

export function importFieldSummary(field:TemplateField):string{return `${field.displayName}${field.isRequired?' *':''}`}
