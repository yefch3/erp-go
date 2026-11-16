import { describe, expect, it } from 'vitest'
import { shippingBusinessLabel } from './shipping'
const t=(key:string,params?:Record<string,unknown>)=>({ 'shipping.unlinkedContract':'未关联合同','shipping.unlinkedBatch':'批次未关联','shipping.transportBatchLabel':`第 ${params?.batch} 批` }[key]||key)
describe('shipping business reference',()=>{
 it('distinguishes actual batches of the same contract',()=>{
  expect(shippingBusinessLabel({contractNo:'CT-TEST',transportBatchNo:2},t)).toBe('CT-TEST · 第 2 批')
  expect(shippingBusinessLabel({contractNo:'CT-TEST',transportBatchNo:3},t)).toBe('CT-TEST · 第 3 批')
 })
 it('does not invent a first batch or a contract for legacy schedules',()=>{
  expect(shippingBusinessLabel({contractNo:'CT-LEGACY'},t)).toBe('CT-LEGACY · 批次未关联')
  expect(shippingBusinessLabel({},t)).toBe('未关联合同 · 批次未关联')
 })
})
