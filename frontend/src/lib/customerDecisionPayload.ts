export type CustomerDecisionForm = {
  selectionId: number
  accepted: boolean
  customerContact: string
  decisionNote: string
  decidedAt: string
  itemPrices: Array<Record<string, any>>
  shipmentPrices: Array<Record<string, any>>
}

export function customerDecisionPayload(form: CustomerDecisionForm) {
  return {
    selection_id: form.selectionId,
    accepted: form.accepted,
    customer_contact: form.customerContact,
    decision_note: form.decisionNote,
    customer_decided_at: form.decidedAt,
    item_prices: form.accepted
      ? form.itemPrices.map(row => ({
          selection_item_id: row.selectionItemId,
          currency: row.currency,
          unit_price: String(row.unitPrice),
          payment_terms: row.paymentTerms,
          incoterm: row.incoterm,
          required_date: row.requiredDate,
        }))
      : [],
    shipment_prices: form.accepted
      ? form.shipmentPrices.map(row => ({
          selection_shipment_id: row.selectionShipmentId,
          currency: row.currency,
          freight_amount: String(row.freightAmount),
        }))
      : [],
  }
}
