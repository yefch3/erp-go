import {describe,expect,it} from 'vitest'
import {blankProduct,type Product} from './inquiryWorkspace'
import type {CategorySelection} from './customerOffer'
import type {TemplateField} from './inquiryTemplates'
import {comparisonHeadingName,comparisonProductDescription,comparisonSpecificationRows,replaceSpecificationSelection,comparisonProductFields,hasMultipleSpecificationSuppliers,type ProductCandidateGroup} from './comparisonSpecifications'

const product=(id:string,name='Steel'):Product=>({...blankProduct(),id,product:name,specification:id,quantity:'10',unit:'MT'})
const group=(name:string,ids:string[]):ProductCandidateGroup=>({key:name,productName:name,category:'FOB_USD',specs:ids.map(id=>({key:id,product:product(id,name),rows:[]})),rows:[]})
const selection=(productId:string,quoteId:string,category='FOB_USD'):CategorySelection=>({productId,quoteId,category,freightQuoteId:'freight',freightQuoteVersion:2,cfrUnitPrice:'550'})

describe('specification comparison rows',()=>{
 it('keeps every specification visible in its owning product group, including many specifications',()=>{
  const steel=group('Steel',Array.from({length:20},(_,i)=>`p${i}`)),zinc=group('Zinc',['z1'])
  const rows=comparisonSpecificationRows([steel,zinc])
  expect(rows).toHaveLength(21)
  expect(rows.slice(0,20).every(row=>row.productGroup===steel)).toBe(true)
  expect(rows[20].productGroup).toBe(zinc)
  expect(rows[19].product.id).toBe('p19')
 })
 it('renders a filtered group without changing its product identity',()=>{
  const filtered=group('Steel',['p2'])
  const rows=comparisonSpecificationRows([filtered])
  expect(rows).toHaveLength(1)
  expect(rows[0]).toMatchObject({product:{id:'p2'},category:'FOB_USD',productGroup:filtered})
 })
 it('replaces multiple old suppliers with one while preserving other specs and categories',()=>{
  const first=selection('p1','a'),second=selection('p1','b'),sibling=selection('p2','a'),cny=selection('p1','c','FOB_CNY')
  const next=replaceSpecificationSelection([first,second,sibling,cny],'p1','FOB_USD','b',['a','b'])
  expect(next).toEqual([sibling,cny,second]);expect(next).toContain(second)
  expect(second.cfrUnitPrice).toBe('550');expect(sibling.freightQuoteId).toBe('freight')
 })
 it('clears only this spec, and creates a new single choice for a new supplier',()=>{
  const sibling=selection('p2','a'),current=[selection('p1','a'),sibling]
  expect(replaceSpecificationSelection(current,'p1','FOB_USD',undefined,['a','b'])).toEqual([sibling])
  expect(replaceSpecificationSelection(current,'p1','FOB_USD','b',['a','b'])).toEqual([sibling,{productId:'p1',category:'FOB_USD',quoteId:'b'}])
  expect(replaceSpecificationSelection(current,'p1','FOB_USD','foreign',['a','b'])).toEqual([sibling])
 })
 it('detects legacy multiple choices per spec and category without modifying them',()=>{
  const current=[selection('p1','a'),selection('p1','b'),selection('p2','a')]
  expect(hasMultipleSpecificationSuppliers(current)).toBe(true)
  expect(current).toHaveLength(3)
  expect(hasMultipleSpecificationSuppliers([selection('p1','a'),selection('p1','c','FOB_CNY'),selection('p2','a')])).toBe(false)
 })
 it('keeps template order followed by all populated supplemental fields',()=>{
  const p={...product('p1'),packaging:'Coil',weight:'10',customFields:{grade:'Q235',finish:'Oiled'}}
  const template=[{fieldKey:'grade',displayName:'Grade',sortOrder:2},{fieldKey:'packaging',displayName:'Packing',sortOrder:1},{fieldKey:'quantity',displayName:'Qty',sortOrder:0}] as TemplateField[]
  const fields=comparisonProductFields(p,template,key=>key)
  expect(fields.map(field=>field.key)).toEqual(['packaging','grade','specification','weight','finish'])
  expect(fields.map(field=>field.value)).toEqual(['Coil','Q235','p1','10','Oiled'])
 })
})

describe('mixed-name description display',()=>{
 const mixed='PPGL COILS AS MTA792 0.28×1220 MM. RAL3002 WITH FILM PVC; 卷重4-4.5吨，覆膜彩涂，膜厚3丝，正面13-15背面5-7'
 it('shows the entire raw name when the specification field is blank',()=>{
  const p={...product('p1',mixed),specification:''}
  expect(comparisonProductDescription(p,[])).toBe(mixed)
  expect(p.product).toBe(mixed)
  expect(comparisonHeadingName(mixed)).toBe('')
 })
 it('appends separate specifications and populated supplemental fields below the raw name',()=>{
  expect(comparisonProductDescription(product('p1',mixed),[{key:'specification',label:'规格',value:'提供材质报告'},{key:'packaging',label:'包装',value:'防潮'}])).toBe(mixed+'\n规格: 提供材质报告\n包装: 防潮')
 })
 it('removes only exactly repeated specification content and repeated instances of the same field',()=>{
  expect(comparisonProductDescription(product('p1',mixed),[{key:'specification',label:'规格',value:mixed},{key:'weight',label:'重量',value:'20'},{key:'weight',label:'重量',value:'20'},{key:'volume',label:'体积',value:'20'}])).toBe(mixed+'\n重量: 20\n体积: 20')
 })
 it('does not guess or remove a specification that is only a substring of the raw name',()=>{
  expect(comparisonProductDescription(product('p1',mixed),[{key:'specification',label:'规格',value:'0.28×1220 MM.'}])).toContain('\n规格: 0.28×1220 MM.')
  expect(comparisonHeadingName('热轧钢卷')).toBe('热轧钢卷')
 })
})
