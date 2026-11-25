type Round = { id: string | number; roundNo: number }

export type QualityFileCategory = 'PHOTO' | 'VIDEO' | 'REPORT' | 'THIRD_PARTY' | 'OTHER'
export interface PendingQualityFile { file: File; category: QualityFileCategory; fileKey?: string }

export function qualityAttachmentPath(taskId: string, fileId: string, orderId?: string): string {
  const task = encodeURIComponent(taskId)
  const file = encodeURIComponent(fileId)
  return orderId
    ? `/purchase-orders/${encodeURIComponent(orderId)}/quality-inspections/${task}/files/${file}`
    : `/quality/tasks/${task}/files/${file}`
}

// Keep the category selected when each file was added, including files waiting for a retry.
export function stageQualityFiles(
  queued: readonly PendingQualityFile[], incoming: readonly File[], category: QualityFileCategory,
): PendingQualityFile[] {
  const next = [...queued]
  const key = (file: File, type: QualityFileCategory) =>
    JSON.stringify([type, file.name, file.size, file.lastModified, file.type])
  const seen = new Set(queued.map(({ file, category: type }) => key(file, type)))
  for (const file of incoming) {
    const id = key(file, category)
    if (seen.has(id)) continue
    next.push({ file, category })
    seen.add(id)
  }
  return next
}

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
