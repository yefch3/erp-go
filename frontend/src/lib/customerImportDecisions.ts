export type CustomerImportAction = 'KEEP' | 'UPDATE'
export type ContactImportAction = 'SKIP' | 'UPDATE' | 'ADD'

export interface CustomerImportVerdictFlags {
  existingCustomer?: boolean
  duplicateContact?: boolean
}

export function applyCustomerImportActions<T extends object>(
  rows: T[],
  verdicts: CustomerImportVerdictFlags[],
  customerAction: CustomerImportAction,
  contactAction: ContactImportAction,
): (T & { customerAction?: CustomerImportAction; contactAction?: ContactImportAction })[] {
  return rows.map((row, index) => ({
    ...row,
    ...(verdicts[index]?.existingCustomer ? { customerAction } : {}),
    ...(verdicts[index]?.duplicateContact ? { contactAction } : {}),
  }))
}
