export interface ExecutionCategoryCalculation {
  exchangeRate: string
  portCharge: string
  inlandFreight: string
  loss: string
  interestRate: string
  interestDays: string
}

export const executionTradeTermFormula: Record<string, string> = {
  FOB_USD: '执行参考单价 = 供应商 FOB 单价 × I',
  FOB_CNY: '执行参考单价 =（供应商 FOB 单价 ÷ 汇率）× I',
  ALL_IN_PORT_CNY: '执行参考单价 =［（一票到港单价 + 港区港杂费单价）÷ 汇率］× I',
  EX_FACTORY_CNY: '执行参考单价 =［（出厂单价 + 产品内陆运费单价 + 港区港杂费单价）÷ 汇率］× I',
  REPROCESSING_CNY: '执行参考单价 =［（再加工单价 + 产品内陆运费单价 + 损耗单价 + 港区港杂费单价）÷ 汇率］× I',
  DIRECT_CFR_USD: '执行参考单价 = 工厂 CFR 单价 × I',
}

function requiredNumber(label: string, value: string, positive = false): number {
  const parsed = Number(value)
  if (value.trim() === '' || !Number.isFinite(parsed) || (positive ? parsed <= 0 : parsed < 0)) {
    throw new Error(`请填写有效的${label}`)
  }
  return parsed
}

export function calculateExecutionReferencePrice(category: string, supplierPrice: string, input: ExecutionCategoryCalculation): number {
  const original = requiredNumber('工厂报价', supplierPrice, true)
  const interestRate = requiredNumber('年利率', input.interestRate)
  const interestDays = requiredNumber('计息天数', input.interestDays)
  const interestFactor = 1 + (interestRate / 100) * interestDays / 360
  let result = original

  if (['FOB_CNY', 'ALL_IN_PORT_CNY', 'EX_FACTORY_CNY', 'REPROCESSING_CNY'].includes(category)) {
    const exchangeRate = requiredNumber('汇率', input.exchangeRate, true)
    let cnyPrice = original
    if (['ALL_IN_PORT_CNY', 'EX_FACTORY_CNY', 'REPROCESSING_CNY'].includes(category)) cnyPrice += requiredNumber('港区港杂费单价', input.portCharge)
    if (['EX_FACTORY_CNY', 'REPROCESSING_CNY'].includes(category)) cnyPrice += requiredNumber('产品内陆运费单价', input.inlandFreight)
    if (category === 'REPROCESSING_CNY') cnyPrice += requiredNumber('损耗单价', input.loss)
    result = cnyPrice / exchangeRate
  }

  const calculated = result * interestFactor
  if (!Number.isFinite(calculated) || calculated <= 0) throw new Error('核算参数无效')
  return calculated
}
