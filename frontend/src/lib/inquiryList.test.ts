import { describe,expect,it } from 'vitest'
import { descriptionSpecification,dimensionFields,dimensionSummary,groupedQuantity,groupInquiryProducts,inquiryListFields,productSummary,specificationAuxiliaryFields,specificationDisplay } from './inquiryList'
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
  it('falls back from size fields to specification text to the product description',()=>{
    const described='HOT ROLLED TEAR DROP / PLANCHAS LAC ESTRIADAS (HRFP) | Size: 2.00 x 1200 x 2400 | QUALITY: ASTM A36/ | COIL I.D.: 700MM/750MM | MAX. WEIGHT: Mínimo 6mt max'
    const bare={...product,product:described,specification:'',customFields:{}}
    expect(specificationDisplay(product)).toEqual({source:'dimensions',value:'2.0 mm × 1250 mm'})
    expect(specificationDisplay({...product,product:described})).toEqual({source:'dimensions',value:'2.0 mm × 1250 mm'})
    expect(specificationDisplay({...bare,specification:' 2.0*1250*C '})).toEqual({source:'specification',value:'2.0*1250*C'})
    expect(specificationDisplay(bare)).toEqual({source:'description',value:'Size: 2.00 x 1200 x 2400 | QUALITY: ASTM A36/ | COIL I.D.: 700MM/750MM | MAX. WEIGHT: Mínimo 6mt max'})
    expect(specificationDisplay({...bare,product:'热轧卷'})).toEqual({source:'none',value:'—'})
  })
  it('takes only what follows the first bar of a product description',()=>{
    expect(descriptionSpecification('PPGI | 0.5 x 1000 | RAL 9002')).toBe('0.5 x 1000 | RAL 9002')
    expect(descriptionSpecification('PPGI|0.5 x 1000')).toBe('0.5 x 1000')
    expect(descriptionSpecification('PPGI | ')).toBe('')
    expect(descriptionSpecification('PPGI 0.5 x 1000')).toBe('')
    expect(descriptionSpecification('')).toBe('')
  })
  it('lists populated template fields beside the size for the quote grid',()=>{
    const template=[{fieldKey:'coil_id',displayName:'卷内径',sortOrder:1,isRequired:false,defaultValue:'',dataType:'TEXT',isCustom:true,isCore:false}]
    const fields=specificationAuxiliaryFields({...product,customFields:{...product.customFields,coil_id:'508'}},template)
    expect(fields.find(field=>field.key==='coil_id')).toEqual({key:'coil_id',label:'卷内径',value:'508'})
    expect(specificationAuxiliaryFields({...product,customFields:{}},template)).toEqual([])
  })
})
