import {describe,it,expect} from 'vitest'
import {buyerFromCustomer,emptyContractParties,missingContractParties} from './contractParties'

describe('contract parties',()=>{
  it('prefills the full customer name and leaves an unknown address blank',()=>{
    expect(buyerFromCustomer({name:' Actual buyer Ltd. '})).toEqual({buyerName:'Actual buyer Ltd.',buyerAddress:''})
  })
  it('requires each field, including whitespace-only values, before submission',()=>{
    const complete={buyerName:'Buyer',buyerAddress:'Buyer address',sellerName:'Seller',sellerAddress:'Seller address'}
    expect(missingContractParties(complete)).toEqual([])
    for(const key of Object.keys(complete))expect(missingContractParties({...complete,[key]:' \n　'})).toHaveLength(1)
    expect(missingContractParties(emptyContractParties())).toHaveLength(4)
  })
})
