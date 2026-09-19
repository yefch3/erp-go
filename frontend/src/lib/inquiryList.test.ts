import { describe,expect,it } from 'vitest'
import { dimensionSummary,inquiryListFields,productSummary } from './inquiryList'
import type { Product } from './inquiryWorkspace'

const product:Product={id:'1',product:'热轧卷',specification:'legacy spec',quantity:'20',unit:'MT',delivery:'',weight:'',volume:'',packaging:'',remark:'',customFields:{material_standard:'ASTM A36',thickness:'2.0',width:'1250',surface_requirement:'涂油'}}

describe('inquiry list product presentation',()=>{
  it('builds a compact summary from fields that already exist',()=>{
    expect(dimensionSummary(product)).toBe('2.0×1250mm')
    expect(productSummary(product)).toBe('热轧卷 / ASTM A36 / 2.0×1250mm')
  })
  it('only adds populated configured fields to expanded details',()=>{
    const fields=inquiryListFields(product,[
      {fieldKey:'surface_requirement',displayName:'表面要求',sortOrder:1,isRequired:false,defaultValue:'',dataType:'TEXT',isCustom:false,isCore:false},
      {fieldKey:'coating',displayName:'涂层',sortOrder:2,isRequired:false,defaultValue:'',dataType:'TEXT',isCustom:false,isCore:false},
    ])
    expect(fields.map(row=>row.value)).toEqual(expect.arrayContaining(['ASTM A36','2.0×1250mm','20','MT','涂油','2.0','1250']))
    expect(fields.some(row=>row.key==='coating')).toBe(false)
  })
})
