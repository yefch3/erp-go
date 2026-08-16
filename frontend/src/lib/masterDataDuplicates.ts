import { ElMessageBox } from 'element-plus'

export interface DuplicateCandidate {
  id: string
  code: string
  name: string
  matchFields?: string[]
}

// 重复预检只提醒用户复核，不代替编码和 UN/LOCODE 等强唯一约束。
export async function confirmPossibleDuplicates(
  candidates: DuplicateCandidate[],
  t: (key: string, params?: Record<string, unknown>) => string,
): Promise<void> {
  if (!candidates.length) return
  const preview = candidates
    .slice(0, 3)
    .map((item) => `${item.code} · ${item.name}`)
    .join('\n')
  const more = Math.max(0, candidates.length - 3)
  await ElMessageBox.confirm(
    `${t('common.duplicateWarning', { count: candidates.length })}\n${preview}${more ? `\n${t('common.duplicateMore', { count: more })}` : ''}`,
    t('common.duplicateTitle'),
    {
      type: 'warning',
      confirmButtonText: t('common.saveAnyway'),
      cancelButtonText: t('common.backToEdit'),
      distinguishCancelAndClose: true,
    },
  )
}
