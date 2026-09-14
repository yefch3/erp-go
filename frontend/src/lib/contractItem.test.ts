import {expect,it} from 'vitest'
import {contractItemPayload} from './contractItem'
it('serializes a manual contract product as numeric zero and preserves all fields',()=>{
 const row={productId:'',productName:'冷轧卷',spec:'1 x 1250',uomCode:'MT',qty:'10',unitPrice:'10.0000'}
 expect(contractItemPayload(row)).toEqual({...row,productId:'0'})
 expect(row.productId).toBe('')
})
it('keeps catalog identifiers exact without converting large int64 values to Number',()=>{
 expect(contractItemPayload({productId:'9223372036854775807'}).productId).toBe('9223372036854775807')
 expect(contractItemPayload({productId:'0'}).productId).toBe('0')
})
