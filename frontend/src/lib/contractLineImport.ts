export const contractImportHeaders = ['产品名称', '规格', '数量', '单位', '对客单价']
export type ContractImportLine = {productId:string;productName:string;spec:string;qty:string;uomCode:string;unitPrice:string}
export function parseContractRows(rows: unknown[][]) {
 const errors:string[]=[];const items:ContractImportLine[]=[]
 const headers=(rows[0]||[]).map(v=>String(v??'').trim())
 const columns=contractImportHeaders.map(h=>headers.indexOf(h))
 for(const h of ['产品名称','数量','单位','对客单价']) {
  if(!headers.includes(h)) errors.push(`缺少“${h}”列，请使用导入模板`)
  else if(headers.filter(v=>v===h).length>1) errors.push(`“${h}”列重复`)
 }
 if(errors.length)return{items,errors}
 for(let i=1;i<rows.length;i++){
  const row=rows[i];if(row.every(v=>v==null||String(v).trim()===''))continue
  const [productName,spec,qty,uomCode,unitPrice]=columns.map(c=>c<0?'':String(row[c]??'').trim())
  const problems:string[]=[]
  if(!productName)problems.push('产品名称为空')
  if(!uomCode)problems.push('单位为空')
  if(!/^\d+(\.\d+)?$/.test(qty)||!Number.isFinite(Number(qty))||Number(qty)<=0)problems.push('数量必须大于 0')
  if(!/^\d+(\.\d+)?$/.test(unitPrice)||!Number.isFinite(Number(unitPrice))||Number(unitPrice)<0)problems.push('对客单价必须为不小于 0 的数字')
  if(!Number.isFinite(Number(qty)*Number(unitPrice)))problems.push('金额超出范围')
  if(problems.length)errors.push(`第 ${i+1} 行：${problems.join('；')}`)
  else items.push({productId:'0',productName,spec,qty,uomCode,unitPrice})
 }
 if(!items.length&&!errors.length)errors.push('工作表没有可导入的产品')
 if(items.length>1000)errors.push('每次最多导入 1000 条产品明细')
 return{items,errors}
}
