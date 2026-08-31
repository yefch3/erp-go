export interface ProcurementReworkForm {
  planId: string | number
  sourcingLineId: string | number
  supplierQuoteLineId: string | number
  requestType: string
  reason: string
}

export function procurementReworkPayload(isSalesView: boolean, form: ProcurementReworkForm) {
  const hasExistingQuote = Number(form.supplierQuoteLineId || 0) > 0
  const requestType = hasExistingQuote
    ? (form.requestType === 'REQUOTE' ? 'REQUOTE' : 'RENEGOTIATE')
    : 'ADD_SUPPLIER'
  const common = {
    sourcing_line_id: Number(form.sourcingLineId || 0),
    supplier_quote_line_id: Number(form.supplierQuoteLineId || 0),
    request_type: requestType,
    reason: String(form.reason || '').trim(),
  }
  return isSalesView
    ? { sales_plan_id: Number(form.planId || 0), ...common }
    : { plan_id: Number(form.planId || 0), ...common }
}

interface SuggestedPriceInput {
  purchaseCurrency: string
  purchaseUnitPrice: string | number
  quantity: string | number
  costCurrency?: string
  confirmedCustomerUnitPrice?: string | number
  shipping?: {
    currency?: string
    chargeBasis?: string
    unitRate?: string | number
    totalFreight?: string | number
  }
}

export interface SuggestedPrice {
  currency: string
  value: string
  source: string
}

function decimal(value: number) {
  return value.toFixed(6).replace(/\.?0+$/, '')
}

export function suggestCustomerUnitPrice(input: SuggestedPriceInput): SuggestedPrice {
  const confirmed = Number(input.confirmedCustomerUnitPrice)
  if (input.confirmedCustomerUnitPrice !== undefined && Number.isFinite(confirmed) && confirmed >= 0) {
    return {
      currency: String(input.costCurrency || input.purchaseCurrency || 'USD').toUpperCase(),
      value: decimal(confirmed),
      source: 'CONFIRMED_COST',
    }
  }

  const purchase = Number(input.purchaseUnitPrice)
  const purchaseValue = Number.isFinite(purchase) && purchase >= 0 ? purchase : 0
  const currency = String(input.purchaseCurrency || 'USD').toUpperCase()
  const shippingCurrency = String(input.shipping?.currency || currency).toUpperCase()
  if (!input.shipping) {
    return {
      currency,
      value: decimal(purchaseValue),
      source: 'PURCHASE_ONLY',
    }
  }
  if (shippingCurrency !== currency) {
    return {
      currency,
      value: decimal(purchaseValue),
      source: 'CURRENCY_MISMATCH',
    }
  }

  const quantity = Number(input.quantity)
  const unitRate = Number(input.shipping.unitRate)
  const totalFreight = Number(input.shipping.totalFreight)
  let freightPerUnit = 0
  if (input.shipping.chargeBasis === 'PER_TON' && Number.isFinite(unitRate) && unitRate >= 0) {
    freightPerUnit = unitRate
  } else if (Number.isFinite(totalFreight) && totalFreight >= 0 && Number.isFinite(quantity) && quantity > 0) {
    freightPerUnit = totalFreight / quantity
  }
  return {
    currency,
    value: decimal(purchaseValue + freightPerUnit),
    source: 'PURCHASE_SHIPPING',
  }
}
