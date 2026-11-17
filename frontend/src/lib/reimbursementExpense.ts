export const expenseTypes = ['TRAVEL', 'OFFICE_SUPPLIES', 'OTHER'] as const
export type ExpenseType = typeof expenseTypes[number]
export function claimExpenseType(value: unknown): string {
  return typeof value === 'string' && value ? value : 'TRAVEL'
}
export function expensePayload<T extends { expenseType: string; expenseDate: string; tripStart: string; tripEnd: string; origin: string; destination: string }>(form: T): T {
  return form.expenseType === 'TRAVEL'
    ? { ...form, expenseDate: '' }
    : { ...form, tripStart: '', tripEnd: '', origin: '', destination: '' }
}
export function missingExpenseFields(form: { expenseType: string; expenseDate: string; tripStart: string; tripEnd: string; origin: string; destination: string; purpose: string; amount: string; paymentAccount: string }): boolean {
  if (!expenseTypes.includes(form.expenseType as ExpenseType)) return true
  const contextMissing = form.expenseType === 'TRAVEL'
    ? !form.tripStart || !form.tripEnd || !form.origin.trim() || !form.destination.trim()
    : !form.expenseDate
  return Boolean(contextMissing || !form.purpose.trim() || !form.amount.trim() || !form.paymentAccount.trim())
}
