import {describe,expect,it} from 'vitest'
import {productsFromImportedSheet,selectImportTemplate} from './inquiryImport'
import type {InquiryTemplate} from './inquiryTemplates'

const fields=[
 {fieldKey:'product',displayName:'产品',sortOrder:1,isRequired:true,defaultValue:'',dataType:'TEXT',isCustom:false,isCore:true},
 {fieldKey:'quantity',displayName:'数量',sortOrder:2,isRequired:true,defaultValue:'',dataType:'NUMBER',isCustom:false,isCore:true},
 {fieldKey:'quantity_unit',displayName:'单位',sortOrder:3,isRequired:true,defaultValue:'PCS',dataType:'TEXT',isCustom:false,isCore:true},
 {fieldKey:'custom.color',displayName:'颜色',sortOrder:4,isRequired:false,defaultValue:'蓝色',dataType:'TEXT',isCustom:true,isCore:false},
]
const template={id:'7',templateCode:'T',version:1,name:'测试格式',description:'',status:'ACTIVE',isDefault:true,isSystem:false,fieldCount:fields.length,fields,createdByName:'',updatedAt:''} satisfies InquiryTemplate

describe('标准询盘导入',()=>{
 it('自动识别模板并按表头而非列顺序映射',()=>{
  expect(selectImportTemplate(['数量','产品'],[template]).id).toBe('7')
  const products=productsFromImportedSheet({name:'Sheet1',columns:['数量','产品'],rows:[['12','螺栓']],totalRows:1},template)
  expect(products[0]).toMatchObject({product:'螺栓',quantity:'12',unit:'PCS',customFields:{'custom.color':'蓝色'}})
 })
 it('拒绝未知表头和超过上限的明细',()=>{
  expect(()=>selectImportTemplate(['产品','客户自建列'],[template])).toThrow('没有定义')
  expect(()=>productsFromImportedSheet({name:'Sheet1',columns:['产品'],rows:[],totalRows:10001},template)).toThrow('10000')
 })
})
