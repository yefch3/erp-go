import {chargeSubtotal} from './inquiryWorkspace'

export interface ContractEditLine {
 productId:string; productName?:string; productCode?:string; skuId?:string;
 spec:string; qty:string; uomCode?:string; unitPrice:string; remark?:string;
}

// Explicit transport fields: never send read-only IDs, totals or opening balances.
export function editableContractLine(line:ContractEditLine):ContractEditLine {
 const {productId,productName,productCode,skuId,spec,qty,uomCode,unitPrice,remark}=line
 return {productId,productName,productCode,skuId,spec,qty,uomCode,unitPrice,remark}
}
export function contractLineAmount(line:{qty:string;unitPrice:string}):string {
 return chargeSubtotal(line.unitPrice,line.qty)||'—'
}
export function contractTotal(lines:{qty:string;unitPrice:string}[]):string {
 let cents=0n
 for(const line of lines){const amount=chargeSubtotal(line.unitPrice,line.qty);if(!amount)return '—';cents+=BigInt(amount.replace('.',''))}
 return `${cents/100n}.${String(cents%100n).padStart(2,'0')}`
}
