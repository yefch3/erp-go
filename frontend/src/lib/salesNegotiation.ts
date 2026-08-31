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
      source: '已带入采购确认的成本方案建议价（含费用与利润，可修改）',
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
      source: '基础参考价：仅采购单价，尚未包含运费、其他费用和利润',
    }
  }
  if (shippingCurrency !== currency) {
    return {
      currency,
      value: decimal(purchaseValue),
      source: `基础参考价：采购与船运币种不同（${currency}/${shippingCurrency}），请确认换算与利润`,
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
    source: '基础参考价：采购单价＋所选船运分摊，尚未包含其他费用和利润',
  }
}
