import {describe,it,expect} from 'vitest'
import {deliveryStatusKey} from './purchaseDelivery'
describe('purchase delivery labels',()=>{
 it('distinguishes supplier handoff from warehouse receipt',()=>{
  expect(deliveryStatusKey({status:'PARTIALLY_RECEIVED',fulfillmentMode:'DIRECT_SHIP'})).toBe('purchaseFulfillment.partialDelivery')
  expect(deliveryStatusKey({status:'RECEIVED',fulfillmentMode:'DIRECT_SHIP'})).toBe('purchaseFulfillment.completeDelivery')
  expect(deliveryStatusKey({status:'PARTIALLY_RECEIVED',fulfillmentMode:'WAREHOUSE'})).toBe('purchaseFulfillment.partialWarehouse')
  expect(deliveryStatusKey({status:'RECEIVED',fulfillmentMode:'WAREHOUSE'})).toBe('purchaseFulfillment.completeWarehouse')
 })
})
