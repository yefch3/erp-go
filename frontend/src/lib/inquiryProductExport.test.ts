import {describe,expect,it} from 'vitest'
import {parseTableFile} from './attachmentExcel'
import {buildInquiryProductsXlsx,exportableInquiryFields,safeInquiryExportName} from './inquiryProductExport'
import type {TemplateField} from './inquiryTemplates'

const fields:TemplateField[]=[
  {fieldKey:'quantity_unit',displayName:'单位',sortOrder:4,isRequired:true,defaultValue:'',dataType:'TEXT',isCustom:false,isCore:true},
  {fieldKey:'quantity',displayName:'数量',sortOrder:3,isRequired:true,defaultValue:'',dataType:'NUMBER',isCustom:false,isCore:true},
  {fieldKey:'product',displayName:'产品',sortOrder:1,isRequired:true,defaultValue:'',dataType:'TEXT',isCustom:false,isCore:true},
  {fieldKey:'custom.grade',displayName:'牌号',sortOrder:2,isRequired:false,defaultValue:'',dataType:'TEXT',isCustom:true,isCore:false},
  {fieldKey:'unit_price',displayName:'单价',sortOrder:5,isRequired:false,defaultValue:'',dataType:'NUMBER',isCustom:false,isCore:true},
]

describe('询盘产品 Excel 导出',()=>{
  it('按模板顺序导出全部产品并保留数值单元格',async()=>{
    const bytes=buildInquiryProductsXlsx([
      {id:'1',product:'热轧钢卷',specification:'',quantity:'25.00',unit:'MT',delivery:'',weight:'',volume:'',packaging:'',remark:'',customFields:{'custom.grade':'A36'}},
      {id:'2',product:'=HYPERLINK("bad")',specification:'',quantity:'500',unit:'PCS',delivery:'',weight:'',volume:'',packaging:'',remark:'',customFields:{'custom.grade':'A500'}},
    ],fields,'产品需求')
    const workbook=await parseTableFile('products.xlsx',bytes.buffer)
    expect(workbook.sheets[0].columns).toEqual(['产品','牌号','数量','单位'])
    expect(workbook.sheets[0].rows).toEqual([
      ['热轧钢卷','A36','25','MT'],
      ['=HYPERLINK("bad")','A500','500','PCS'],
    ])
  })

  it('不导出询盘阶段的价格列并清理文件名',()=>{
    expect(exportableInquiryFields(fields).map(field=>field.fieldKey)).toEqual(['product','custom.grade','quantity','quantity_unit'])
    expect(safeInquiryExportName('INQ-260920-002')).toBe('INQ-260920-002-products.xlsx')
    expect(safeInquiryExportName('bad/name:*')).toBe('bad-name---products.xlsx')
  })
})
