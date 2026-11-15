export interface ManualCargoLine { productCode:string; productName:string; specification:string; quantity:string; uomCode:string; remark:string }
export function manualCargoLines(rows:ManualCargoLine[]) {
  return rows.map(row=>Object.fromEntries(Object.entries(row).map(([key,value])=>[key,String(value||'').trim()])) as unknown as ManualCargoLine)
    .filter(row=>Object.values(row).some(Boolean))
}

export function manualCargoError(rows:ManualCargoLine[]):string {
  for (const [index,line] of manualCargoLines(rows).entries()) {
    if (!line.productName||!line.uomCode||!/^\d+(\.\d{1,4})?$/.test(line.quantity)||Number(line.quantity)<=0) return `第 ${index+1} 行请填写产品名称、单位和大于 0 的数量（最多四位小数）`
  }
  return ''
}
