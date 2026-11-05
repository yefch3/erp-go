import { describe, expect, it } from 'vitest'
import { contractVersionPresentation, isPendingContractChange } from './contractVersionPresentation'

const contract = { entrySource: 'STANDARD', status: 'DRAFT', versionNo: 2, currentVersionId: '11', signedAt: '2026-11-05', conditionConfirmedAt: '' }
const signed = { id: '11', versionNo: 1, status: 'APPROVED', totalAmount: '50782.50', currency: 'USD' }
const change = { id: '12', versionNo: 2, status: 'DRAFT', totalAmount: '48350.00', currency: 'USD' }

describe('contract version labels without changing workflow', () => {
  it('shows both amounts and version identities during an unsigned change', () => {
    expect(isPendingContractChange(contract)).toBe(true)
    expect(contractVersionPresentation(contract, [signed, change])).toEqual({ phase: 'pending', signed, latest: change })
  })
  it('does not mislabel an initial contract draft as a change', () => {
    expect(isPendingContractChange({ ...contract, versionNo: 1, signedAt: '', currentVersionId: '0' })).toBe(false)
    expect(contractVersionPresentation({ ...contract, currentVersionId: '0' }, [change])).toBeNull()
  })
  it('shows the finance requirement after the new version is signed and stops after release', () => {
    const approved = { ...change, status: 'APPROVED' }
    const executing = { ...contract, status: 'EXECUTING', currentVersionId: '12' }
    expect(contractVersionPresentation(executing, [signed, approved])).toMatchObject({ phase: 'finance', signed: approved, latest: approved })
    expect(contractVersionPresentation({ ...executing, conditionConfirmedAt: '2026-11-06' }, [signed, approved])).toBeNull()
  })
  it.each(['HISTORICAL_RECORD', 'EXISTING_CONTRACT'])('leaves %s presentation unchanged', entrySource => {
    expect(isPendingContractChange({ ...contract, entrySource })).toBe(false)
    expect(contractVersionPresentation({ ...contract, entrySource }, [signed, change])).toBeNull()
  })
  it('does not show a rejected or completed change as pending', () => {
    expect(contractVersionPresentation(contract, [signed, { ...change, status: 'REJECTED' }])).toBeNull()
    expect(isPendingContractChange({ ...contract, status: 'COMPLETED' })).toBe(false)
  })
})
