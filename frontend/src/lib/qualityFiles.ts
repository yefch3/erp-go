type Round = { id: string | number; roundNo: number }

// Match the persisted round ID. File order and the current task status cannot
// establish which inspection an older attachment belongs to.
export function qualityFileRound(roundId: string | number | undefined, rounds: readonly Round[]): {
  key: string; params?: { n: number }
} {
  if (!roundId || String(roundId) === '0') return { key: 'quality.fileRoundWhole' }
  const round = rounds.find(item => String(item.id) === String(roundId))
  if (!round || !Number.isInteger(round.roundNo) || round.roundNo < 1) {
    return { key: 'quality.fileRoundUnknown' }
  }
  return round.roundNo === 1
    ? { key: 'quality.fileRoundInitial' }
    : { key: 'quality.fileRoundRetest', params: { n: round.roundNo - 1 } }
}
