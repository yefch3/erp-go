export interface ContractParties {
  buyerName: string
  buyerAddress: string
  sellerName: string
  sellerAddress: string
}

export const emptyContractParties = (): ContractParties => ({buyerName:'', buyerAddress:'', sellerName:'', sellerAddress:''})

export function buyerFromCustomer(customer?: {name?: string; address?: string}) {
  // A contract needs the full customer name, never a list label or short name.
  return {buyerName: customer?.name?.trim() || '', buyerAddress: customer?.address?.trim() || ''}
}

export function missingContractParties(parties: ContractParties): string[] {
  return (Object.entries({buyerName:'买方抬头', buyerAddress:'买方合同地址', sellerName:'卖方抬头', sellerAddress:'卖方合同地址'}) as [keyof ContractParties,string][])
    .filter(([key]) => !parties[key]?.trim()).map(([,label]) => label)
}
