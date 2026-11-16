type ContractState = { entrySource: string; status: string; versionNo: number; signedAt?: string; currentVersionId?: string; conditionConfirmedAt?: string }
type VersionState = { id: string; versionNo: number; status: string; totalAmount: string; currency: string }

// Presentation only: never used to authorize actions or allow execution.
export function isPendingContractChange(contract: ContractState): boolean {
  return contract.entrySource === 'STANDARD' && contract.versionNo > 1 &&
    (!!contract.signedAt || (!!contract.currentVersionId && contract.currentVersionId !== '0')) &&
    ['DRAFT', 'PENDING_APPROVAL', 'PENDING_SIGN'].includes(contract.status)
}

export function contractVersionPresentation(contract: ContractState, versions: VersionState[]) {
  if (contract.entrySource !== 'STANDARD') return null
  const latest = versions.reduce<VersionState | undefined>((a, v) => !a || v.versionNo > a.versionNo ? v : a, undefined)
  const signed = versions.find(v => v.id === contract.currentVersionId)
  if (!latest || !signed || latest.versionNo < 2) return null
  if (latest.id !== signed.id && ['DRAFT', 'PENDING_APPROVAL', 'APPROVED'].includes(latest.status)) {
    return { phase: 'pending' as const, latest, signed }
  }
  if (latest.id === signed.id && ['EXECUTING', 'EFFECTIVE'].includes(contract.status) && !contract.conditionConfirmedAt) {
    return { phase: 'finance' as const, latest, signed }
  }
  return null
}
