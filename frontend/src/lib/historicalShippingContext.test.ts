import { describe, expect, it } from 'vitest'
import { readFileSync } from 'node:fs'
import { historicalShippingContext } from './historicalShippingContext'
import { historicalShippingZh, historicalShippingEn, historicalShippingEs } from '../locales/historicalShipping'

describe('historical purchase shipping context', () => {
  it('keeps missing, old and blank records optional', () => {
    for (const value of [undefined, null, {}, {exportContractNo: ' ', destinationPort: '\n'}]) {
      expect(historicalShippingContext(value)).toEqual({exportContractNo: '', destinationPort: ''})
    }
  })
  it('preserves independent text fields, and never sends financial or workflow fields', () => {
    const order = {id: '99', exportContractNo: ' CT-609 ', destinationPort: ' Callao, Peru ', status: 'RECEIVED', unitPrice: '123', remark: 'other'}
    expect(historicalShippingContext(order)).toEqual({exportContractNo: 'CT-609', destinationPort: 'Callao, Peru'})
    expect(historicalShippingContext({destinationPort: '<b>港口</b>'}).destinationPort).toBe('<b>港口</b>')
  })
  it('provides complete Chinese, English and Spanish labels', () => {
    for (const messages of [historicalShippingEn, historicalShippingEs]) {
      expect(Object.keys(messages).sort()).toEqual(Object.keys(historicalShippingZh).sort())
      for (const message of Object.values(messages)) expect(message.trim()).not.toBe('')
    }
  })
  it('wires both optional fields into the full historical form and its existing rehydration', () => {
    const source = readFileSync(new URL('../components/HistoricalPurchaseOrderDialog.vue', import.meta.url), 'utf8')
    for (const [key, label, length] of [['exportContractNo', 'optionalContract', '50'], ['destinationPort', 'optionalPort', '200']]) {
      const item = source.match(new RegExp(`<el-form-item :label="t\\('historicalShipping\\.${label}'\\)"[^>]*>([\\s\\S]*?)</el-form-item>`))
      expect(item?.[1]).toContain(`v-model="form.${key}"`)
      expect(item?.[1]).toContain(`maxlength="${length}"`)
      expect(item?.[0]).not.toMatch(/\brequired\b/)
      expect(source).toContain(`${key}:''`)
    }
    expect(source).toContain('Object.keys(empty())')
    expect(source).toContain('({...form,supplierId:')
  })
})
