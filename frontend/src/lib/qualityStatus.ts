const inspectionStatuses = ['WAITING', 'IN_PROGRESS', 'REINSPECTION', 'COMPLETED'] as const

// The task lifecycle is independent from the list's pending/completed tabs.
export function qualityStatusKey(status?: string): string | undefined {
  return inspectionStatuses.some(value => value === status)
    ? `quality.statuses.${status}`
    : undefined
}
