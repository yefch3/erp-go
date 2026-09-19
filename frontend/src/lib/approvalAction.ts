import { get } from '../api'

export interface ActionableApproval {
  taskId: string
  override: boolean
  assigneeId: string
}

interface ActionableTaskResponse {
  task?: { id?: string; assigneeId?: string }
  override?: boolean
}

// The backend is the single source of truth for both the assigned approver
// and SUPER_ADMIN takeover. An empty task is a normal "no action available"
// response, not an error.
export async function getActionableApproval(bizType: string, bizId: string | number): Promise<ActionableApproval | null> {
  const data = await get<ActionableTaskResponse>('/approvals/actionable-task', {
    biz_type: bizType,
    biz_id: bizId,
  })
  const taskId = String(data.task?.id ?? '')
  if (!taskId) return null
  return {
    taskId,
    override: Boolean(data.override),
    assigneeId: String(data.task?.assigneeId ?? ''),
  }
}
