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
  return (Object.entries({buyerName:'买方公司名称', buyerAddress:'买方公司地址', sellerName:'卖方公司名称', sellerAddress:'卖方公司地址'}) as [keyof ContractParties,string][])
    .filter(([key]) => !parties[key]?.trim()).map(([,label]) => label)
}
