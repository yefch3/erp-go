import {describe,it,expect} from 'vitest'
import {contractImportHeaders as headers,parseContractRows} from './contractLineImport'
describe('contract line import',()=>{
 it('maps reordered columns and preserves specifications',()=>{
 const result=parseContractRows([['单位','对客单价','数量','规格','产品名称','金额'],['PCS',12.5,2,'001-A','产品甲',999]])
 expect(result.errors).toEqual([]);expect(result.items[0]).toEqual({productId:'0',productName:'产品甲',spec:'001-A',qty:'2',uomCode:'PCS',unitPrice:'12.5'})
 })
 it('allows optional spec and zero price',()=>{expect(parseContractRows([['产品名称','数量','单位','对客单价'],['样品','1','PCS','0']]).errors).toEqual([])})
 it('reports Excel row numbers and skips blank rows',()=>{
 const result=parseContractRows([headers,[],['产品','','-2','PCS','3'],['产品','','2','','not-a-price']]);expect(result.errors[0]).toContain('第 3 行');expect(result.errors[1]).toContain('第 4 行');expect(result.items).toHaveLength(0)
 })
 it('rejects ambiguous and missing headers',()=>{expect(parseContractRows([['产品名称','数量','数量','单位'],['p','1','2','PCS']]).errors).toHaveLength(2)})
 it('rejects excessive rows and empty sheets',()=>{expect(parseContractRows([headers]).errors.length).toBeGreaterThan(0);expect(parseContractRows([headers,...Array.from({length:1001},()=>['p','','1','PCS','1'])]).errors).toContain('每次最多导入 1000 条产品明细')})
})
