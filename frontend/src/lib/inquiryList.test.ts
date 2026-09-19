import { describe,expect,it } from 'vitest'
import { dimensionFields,dimensionSummary,groupedQuantity,groupInquiryProducts,inquiryListFields,productSummary,specificationAuxiliaryFields } from './inquiryList'
import type { Product } from './inquiryWorkspace'

const product:Product={id:'1',product:'热轧卷',specification:'legacy spec',quantity:'20',unit:'MT',delivery:'',weight:'',volume:'',packaging:'',remark:'',customFields:{material_standard:'ASTM A36',thickness:'2.0',width:'1250',surface_requirement:'涂油'}}

describe('inquiry list product presentation',()=>{
  it('builds a compact summary from fields that already exist',()=>{
    expect(dimensionFields(product).map(field=>field.key)).toEqual(['thickness','width'])
    expect(dimensionSummary(product)).toBe('2.0 mm × 1250 mm')
    expect(productSummary(product)).toBe('热轧卷 / ASTM A36 / 2.0 mm × 1250 mm')
  })
  it('only adds populated configured fields to expanded details',()=>{
    const fields=inquiryListFields(product,[
      {fieldKey:'surface_requirement',displayName:'表面要求',sortOrder:1,isRequired:false,defaultValue:'',dataType:'TEXT',isCustom:false,isCore:false},
      {fieldKey:'coating',displayName:'涂层',sortOrder:2,isRequired:false,defaultValue:'',dataType:'TEXT',isCustom:false,isCore:false},
    ])
    expect(fields.map(row=>row.value)).toEqual(expect.arrayContaining(['ASTM A36','2.0 mm × 1250 mm','20','MT','涂油','2.0','1250']))
    expect(fields.some(row=>row.key==='coating')).toBe(false)
  })
  it('groups many lines by product and then by identical specification',()=>{
    const duplicate={...product,id:'2',quantity:'5',customFields:{...product.customFields}}
    const changed={...product,id:'3',quantity:'7',customFields:{...product.customFields,width:'1500'}}
    const pipe={...product,id:'4',product:'矩形钢管',quantity:'10',unit:'PCS'}
    const groups=groupInquiryProducts([product,duplicate,changed,pipe])
    expect(groups).toHaveLength(2)
    expect(groups[0].specifications).toHaveLength(2)
    expect(groupedQuantity(groups[0].specifications[0].products)).toBe('25.00 MT')
    expect(specificationAuxiliaryFields(product).map(field=>field.key)).toContain('surface_requirement')
    expect(specificationAuxiliaryFields(product).map(field=>field.key)).not.toContain('width')
  })
  it('keeps a fifty-line inquiry compact at the product level',()=>{
    const products=Array.from({length:50},(_,index)=>({...product,id:String(index+1),product:`产品 ${index%5+1}`,quantity:'1',customFields:{...product.customFields,width:String(1000+index%10)}}))
    const groups=groupInquiryProducts(products)
    expect(groups).toHaveLength(5)
    expect(groups.every(group=>group.products.length===10)).toBe(true)
    expect(groups.every(group=>group.specifications.length===2)).toBe(true)
  })
})
