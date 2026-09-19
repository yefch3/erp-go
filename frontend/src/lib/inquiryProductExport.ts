import type {TemplateField} from './inquiryTemplates'
import {productTemplateValue,type Product} from './inquiryWorkspace'
import {buildXlsx} from './simpleXlsx'

const fallbackFields:TemplateField[]=[
  {fieldKey:'product',displayName:'产品',sortOrder:1,isRequired:true,defaultValue:'',dataType:'TEXT',isCustom:false,isCore:true},
  {fieldKey:'specification',displayName:'规格',sortOrder:2,isRequired:false,defaultValue:'',dataType:'TEXT',isCustom:false,isCore:false},
  {fieldKey:'quantity',displayName:'数量',sortOrder:3,isRequired:true,defaultValue:'',dataType:'NUMBER',isCustom:false,isCore:true},
  {fieldKey:'quantity_unit',displayName:'单位',sortOrder:4,isRequired:true,defaultValue:'',dataType:'TEXT',isCustom:false,isCore:true},
  {fieldKey:'delivery',displayName:'交期',sortOrder:5,isRequired:false,defaultValue:'',dataType:'DATE',isCustom:false,isCore:false},
  {fieldKey:'weight',displayName:'重量',sortOrder:6,isRequired:false,defaultValue:'',dataType:'NUMBER',isCustom:false,isCore:false},
  {fieldKey:'volume',displayName:'体积',sortOrder:7,isRequired:false,defaultValue:'',dataType:'NUMBER',isCustom:false,isCore:false},
  {fieldKey:'packaging',displayName:'包装',sortOrder:8,isRequired:false,defaultValue:'',dataType:'TEXT',isCustom:false,isCore:false},
  {fieldKey:'package_quantity',displayName:'包装数量',sortOrder:9,isRequired:false,defaultValue:'',dataType:'NUMBER',isCustom:false,isCore:false},
  {fieldKey:'remarks',displayName:'备注',sortOrder:10,isRequired:false,defaultValue:'',dataType:'TEXT',isCustom:false,isCore:false},
]

export function exportableInquiryFields(fields?:TemplateField[]):TemplateField[]{
  return (fields?.length?fields:fallbackFields).filter(field=>!['unit_price','total_price'].includes(field.fieldKey)).slice().sort((a,b)=>a.sortOrder-b.sortOrder)
}

function cellValue(product:Product,field:TemplateField):string|number{
  const value=productTemplateValue(product,field.fieldKey)
  if(field.dataType==='NUMBER'&&/^-?(?:\d+\.?\d*|\.\d+)$/.test(value.trim()))return Number(value)
  return value
}

export function buildInquiryProductsXlsx(products:Product[],fields:TemplateField[]|undefined,sheetName:string):Uint8Array<ArrayBuffer>{
  const columns=exportableInquiryFields(fields)
  const rows:(string|number)[][]=[
    columns.map(field=>field.displayName),
    ...products.map(product=>columns.map(field=>cellValue(product,field))),
  ]
  return buildXlsx(sheetName,rows)
}

export function safeInquiryExportName(number:string):string{
  const safe=number.trim().replace(/[<>:"/\\|?*\u0000-\u001f]/g,'-').replace(/[. ]+$/g,'')||'inquiry'
  return `${safe}-products.xlsx`
}
