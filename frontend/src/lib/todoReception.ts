import { get, put, quietErrors } from '../api'

export const todoCategories = ['SALES_INQUIRY', 'FACTORY_INQUIRY', 'LOGISTICS_INQUIRY', 'EXECUTION_PROCUREMENT', 'QUALITY'] as const
export type TodoCategory = typeof todoCategories[number]

export interface TodoReception {
  approvalsOnly: boolean
  categories: TodoCategory[]
  employeeOverride: boolean
}

export function receivesTodo(reception: TodoReception | null | undefined, category: TodoCategory) {
  return !!reception && !reception.approvalsOnly && reception.categories.includes(category)
}

export async function getTodoReception(path: string) {
  const { reception } = await get<{ reception: TodoReception }>(path, undefined, quietErrors)
  return reception
}

export async function saveTodoReception(path: string, approvalsOnly: boolean, categories?: TodoCategory[]) {
  const { reception } = await put<{ reception: TodoReception }>(path, { approvalsOnly, categories })
  return reception
}
