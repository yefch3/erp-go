import {describe,it,expect} from 'vitest'
import {applyTemplateDefaults,blankProduct,pastePrices,productTemplateValue,setProductTemplateValue} from './inquiryWorkspace'
describe('inquiry quote Excel paste',()=>{
 it('preserves existing values and maps past a page boundary using stable product IDs',()=>{
 const products=Array.from({length:126},(_,i)=>({...blankProduct(),id:String(i+1)}))
 const out=pastePrices('12.10\t2026-10-01\tA\n13.20\t2026-10-02\tB',products,[{productId:'1',price:'4',delivery:'',remark:''}],'20')
 expect(out).toEqual([{productId:'1',price:'4',delivery:'',remark:''},{productId:'20',price:'12.10',delivery:'2026-10-01',remark:'A'},{productId:'21',price:'13.20',delivery:'2026-10-02',remark:'B'}])
 })
 it('respects filtered order without overwriting unrelated rows',()=>{const products=['8','11'].map(id=>({...blankProduct(),id}));const out=pastePrices('5\n6',products,[],'8');expect(out.map(p=>p.productId)).toEqual(['8','11'])})
})

import {chargeSubtotal,chargeTotals} from './inquiryWorkspace'
it('previews decimal charges with per-line half-up rounding and separate currencies',()=>{
 expect(chargeSubtotal('0.1','3')).toBe('0.30')
 expect(chargeSubtotal('1.005','1')).toBe('1.01')
 expect(chargeSubtotal('999999999999.99','3')).toBe('2999999999999.97')
 expect(chargeTotals([{amount:'10.05',quantity:'3',currency:'usd'},{amount:'21.11',quantity:'2',currency:'CNY'}] as any)).toEqual({USD:'30.15',CNY:'42.22'})
})

import {productTotal} from './inquiryWorkspace'
it('keeps source quantities with different units separate and flags incomplete weights',()=>{
 const products=[{...blankProduct(),quantity:'0.1',unit:'MT',weight:'2 kg'},{...blankProduct(),quantity:'0.2',unit:'MT',weight:''},{...blankProduct(),quantity:'3',unit:'PC',weight:'4 kg'}]
 expect(productTotal(products,'quantity','unit')).toBe('0.3 MT；3 PC')
 expect(productTotal(products,'weight')).toBe('6 kg（部分未填写）')
})

describe('template-driven inquiry products',()=>{
 it('maps core and custom columns without losing hidden values',()=>{
  const product=blankProduct()
  setProductTemplateValue(product,'product','热轧钢卷')
  setProductTemplateValue(product,'quantity_unit','MT')
  setProductTemplateValue(product,'custom.height_mm','1250')
  expect(product.product).toBe('热轧钢卷')
  expect(product.unit).toBe('MT')
  expect(productTemplateValue(product,'custom.height_mm')).toBe('1250')
 })
 it('applies template defaults only to empty cells',()=>{
  const product=blankProduct();product.unit='PCS'
  applyTemplateDefaults(product,[
   {fieldKey:'quantity_unit',displayName:'单位',sortOrder:1,isRequired:true,defaultValue:'MT',dataType:'TEXT',isCustom:false,isCore:true},
   {fieldKey:'custom.color',displayName:'颜色',sortOrder:2,isRequired:false,defaultValue:'蓝色',dataType:'TEXT',isCustom:true,isCore:false},
  ])
  expect(product.unit).toBe('PCS')
  expect(product.customFields['custom.color']).toBe('蓝色')
 })
})
