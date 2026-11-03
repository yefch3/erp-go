import type { FreightRate, Product } from './inquiryWorkspace'

// 物流报价「适用货物」表的一键填写：表头全选、「统一填写」条、撤销一次、保存前查空单价。
// 只认表里现有的产品；询盘删过产品留下的旧行不碰，也不算进勾选数。

export type FreightFillKey = 'price' | 'currency' | 'unit'
export type FreightFillInput = Record<FreightFillKey, string>
export type FreightFillPatch = Partial<Record<FreightFillKey, string>>
export type FreightFillProblem = 'empty' | 'price' | 'currency'
export interface FreightSelection { checked: boolean; indeterminate: boolean; count: number }
export interface FreightFillUndo { added: string[]; changes: { productId: string; key: FreightFillKey; before: string }[] }
export interface FreightFillResult { rates: FreightRate[]; undo: FreightFillUndo; count: number }

const FILL_KEYS: FreightFillKey[] = ['price', 'currency', 'unit']
// 后端用 decimal 解析且不许负数；「1,200」这种也会被它拒，所以这里一起拦。
const PRICE_PATTERN = /^(?:\d+(?:\.\d+)?|\.\d+)$/
const CURRENCY_PATTERN = /^[A-Z]{3}$/

const tableIds = (products: Product[]) => new Set(products.map(product => product.id))

// 和逐行勾选（toggleFreightProduct）给的默认值保持一致。
export function blankFreightRate(product: Product, currency: string): FreightRate {
  return { productId: product.id, price: '', currency: currency || 'USD', unit: product.unit || 'MT', remark: '' }
}

export function freightSelection(products: Product[], rates: FreightRate[]): FreightSelection {
  const ticked = new Set(rates.map(rate => rate.productId))
  const count = products.filter(product => ticked.has(product.id)).length
  return { checked: count > 0 && count === products.length, indeterminate: count > 0 && count < products.length, count }
}

// 已勾的行原样留着（同一个对象），没勾的按表格顺序补在后面。
export function selectAllFreightRates(products: Product[], rates: FreightRate[], currency: string): { rates: FreightRate[]; added: string[] } {
  const ticked = new Set(rates.map(rate => rate.productId))
  const missing = products.filter(product => !ticked.has(product.id))
  return { rates: [...rates, ...missing.map(product => blankFreightRate(product, currency))], added: missing.map(product => product.id) }
}

export function unselectAllFreightRates(products: Product[], rates: FreightRate[]): FreightRate[] {
  const ids = tableIds(products)
  return rates.filter(rate => !ids.has(rate.productId))
}

// 取消全选会丢掉的、已经填了单价的行：有的话先问一声。
export function pricedFreightRates(products: Product[], rates: FreightRate[]): FreightRate[] {
  const ids = tableIds(products)
  return rates.filter(rate => ids.has(rate.productId) && rate.price.trim() !== '')
}

// 留空的框表示「这一列不改」。币种统一转大写。
export function freightFillPatch(input: FreightFillInput): { patch: FreightFillPatch } | { error: FreightFillProblem } {
  const price = input.price.trim(), currency = input.currency.trim().toUpperCase(), unit = input.unit.trim()
  if (!price && !currency && !unit) return { error: 'empty' }
  if (price && !PRICE_PATTERN.test(price)) return { error: 'price' }
  if (currency && !CURRENCY_PATTERN.test(currency)) return { error: 'currency' }
  const patch: FreightFillPatch = {}
  if (price) patch.price = price
  if (currency) patch.currency = currency
  if (unit) patch.unit = unit
  return { patch }
}

// 默认覆盖已填的值；记下每个改过的格子原来是什么，以及这次替人勾上的行，撤销时还原。
export function fillFreightTable(products: Product[], rates: FreightRate[], patch: FreightFillPatch, options: { selectAll: boolean; currency: string }): FreightFillResult {
  const selected = options.selectAll ? selectAllFreightRates(products, rates, options.currency) : { rates, added: [] as string[] }
  const ids = tableIds(products), added = new Set(selected.added)
  const changes: FreightFillUndo['changes'] = []
  let count = 0
  const next = selected.rates.map(rate => {
    if (!ids.has(rate.productId)) return rate
    count++
    const updated = { ...rate }
    for (const key of FILL_KEYS) {
      const value = patch[key]
      if (value === undefined || rate[key] === value) continue
      if (!added.has(rate.productId)) changes.push({ productId: rate.productId, key, before: rate[key] })
      updated[key] = value
    }
    return updated
  })
  return { rates: next, undo: { added: selected.added, changes }, count }
}

// 只撤这一次填写：之后又被取消勾选的行跳过，别的格子后来的修改保留。
export function undoFreightFill(rates: FreightRate[], undo: FreightFillUndo): FreightRate[] {
  const added = new Set(undo.added)
  return rates.filter(rate => !added.has(rate.productId)).map(rate => {
    const changes = undo.changes.filter(change => change.productId === rate.productId)
    return changes.length ? changes.reduce((restored, change) => ({ ...restored, [change.key]: change.before }), rate) : rate
  })
}

// 后端遇到空单价只说「请填写有效的产品海运单价……」，不说是哪一行；按表格顺序列出来。
export function missingFreightPrices(products: Product[], rates: FreightRate[]): Product[] {
  const blank = new Set(rates.filter(rate => rate.price.trim() === '').map(rate => rate.productId))
  return products.filter(product => blank.has(product.id))
}
