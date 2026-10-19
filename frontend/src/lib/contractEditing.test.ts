import {expect,it} from 'vitest'
import {editableContractLine,contractLineAmount,contractTotal} from './contractEditing'
it('preserves SKU and remarks without posting read-only identifiers',()=>{
 const input={id:'999',productId:'0',productName:'HRC',productCode:'P',skuId:'12',spec:'3mm',qty:'10',uomCode:'MT',unitPrice:'2',remark:'dry packing'}
 expect(editableContractLine(input)).toEqual({productId:'0',productName:'HRC',productCode:'P',skuId:'12',spec:'3mm',qty:'10',uomCode:'MT',unitPrice:'2',remark:'dry packing'})
})
it('rounds each amount in decimal before summing',()=>{
 expect(contractLineAmount({qty:'1',unitPrice:'1.005'})).toBe('1.01')
 expect(contractTotal([{qty:'1',unitPrice:'1.005'},{qty:'1',unitPrice:'2.675'}])).toBe('3.69')
 expect(contractTotal([{qty:'',unitPrice:'2'}])).toBe('—')
})
