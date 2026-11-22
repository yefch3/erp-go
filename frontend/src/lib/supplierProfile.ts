export interface SupplierProfile {
  nameZh: string
  nameEn: string
  shortName: string
  countryCode: string
  currency: string
  taxId: string
  businessTypes: string[]
  paymentTerm: string
  registeredAddress: string
  address: string
  remark: string
}

export function supplierProfile(source: Partial<SupplierProfile> = {}): SupplierProfile {
  return {
    nameZh: source.nameZh || '',
    nameEn: source.nameEn || '',
    shortName: source.shortName || '',
    countryCode: source.countryCode || '',
    currency: source.currency || '',
    taxId: source.taxId || '',
    businessTypes: [...(source.businessTypes || [])],
    paymentTerm: source.paymentTerm || '',
    registeredAddress: source.registeredAddress || '',
    address: source.address || '',
    remark: source.remark || '',
  }
}
