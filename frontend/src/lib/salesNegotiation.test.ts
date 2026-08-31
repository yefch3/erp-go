import { describe, expect, it } from 'vitest'
import { procurementReworkPayload, suggestCustomerUnitPrice } from './salesNegotiation'

describe('sales negotiation helpers', () => {
  it('uses only sales_plan_id for a sales-side procurement rework request', () => {
    const payload = procurementReworkPayload(true, {
      planId: 19,
      sourcingLineId: 7,
      supplierQuoteLineId: 31,
      requestType: 'RENEGOTIATE',
      reason: ' 价格过高 ',
    })
    expect(payload).toEqual({
      sales_plan_id: 19,
      sourcing_line_id: 7,
      supplier_quote_line_id: 31,
      request_type: 'RENEGOTIATE',
      reason: '价格过高',
    })
    expect(payload).not.toHaveProperty('plan_id')
  })

  it('keeps plan_id only on the procurement-side request', () => {
    const payload = procurementReworkPayload(false, {
      planId: 9,
      sourcingLineId: 0,
      supplierQuoteLineId: 0,
      requestType: 'ADD_SUPPLIER',
      reason: '增加一家供应商',
    })
    expect(payload).toHaveProperty('plan_id', 9)
    expect(payload).not.toHaveProperty('sales_plan_id')
  })

  it('forces add-supplier semantics when no existing quote is selected', () => {
    const payload = procurementReworkPayload(true, {
      planId: 19,
      sourcingLineId: 7,
      supplierQuoteLineId: 0,
      requestType: 'RENEGOTIATE',
      reason: '现有供应商太贵',
    })
    expect(payload).toMatchObject({
      supplier_quote_line_id: 0,
      request_type: 'ADD_SUPPLIER',
    })
  })

  it('prefers a confirmed cost-plan selling price', () => {
    expect(suggestCustomerUnitPrice({
      purchaseCurrency: 'USD', purchaseUnitPrice: '2', quantity: '20',
      costCurrency: 'USD', confirmedCustomerUnitPrice: '15.5',
      shipping: { currency: 'USD', chargeBasis: 'PER_TON', unitRate: '10', totalFreight: '200' },
    })).toMatchObject({ currency: 'USD', value: '15.5', source: 'CONFIRMED_COST' })
  })

  it('falls back to purchase plus allocated shipping when no cost plan exists', () => {
    expect(suggestCustomerUnitPrice({
      purchaseCurrency: 'USD', purchaseUnitPrice: '2', quantity: '20',
      shipping: { currency: 'USD', chargeBasis: 'PER_TON', unitRate: '10', totalFreight: '200' },
    })).toMatchObject({ currency: 'USD', value: '12', source: 'PURCHASE_SHIPPING' })
  })
})
