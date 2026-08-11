export interface EmployeeFormInput {
  id: string
  code: string
  name: string
  departmentId: string
  position: string
  email: string
  phone: string
  managerId: string
  englishName: string
  hireDate: string
  leaveDate: string
  remark: string
  version: number
  username: string
  initialPassword: string
}

export type EmployeeValidationError =
  | 'required'
  | 'emailInvalid'
  | 'phoneInvalid'
  | 'accountIncomplete'
  | 'passwordTooShort'
  | 'leaveBeforeHire'

// 前端先做与 IAM 服务一致的基础校验，让员工在提交前就能看到可修正的问题。
export function validateEmployeeForm(form: EmployeeFormInput, editing: boolean): EmployeeValidationError | null {
  if (!form.code.trim() || !form.name.trim() || !form.departmentId) return 'required'

  const email = form.email.trim()
  if (email && !/^[^\s@]+@[^\s@]+\.[^\s@]+$/.test(email)) return 'emailInvalid'

  const phone = form.phone.trim()
  if (phone && (phone.length < 6 || phone.length > 30 || !/^[0-9+()\-. ]+$/.test(phone))) {
    return 'phoneInvalid'
  }

  if (!editing) {
    if (!form.username !== !form.initialPassword) return 'accountIncomplete'
    if (form.initialPassword && form.initialPassword.length < 10) return 'passwordTooShort'
  }

  if (editing && form.hireDate && form.leaveDate && form.leaveDate < form.hireDate) return 'leaveBeforeHire'
  return null
}

function employeeSharedBody(form: EmployeeFormInput) {
  return {
    code: form.code,
    name: form.name,
    departmentId: form.departmentId,
    position: form.position,
    email: form.email,
    phone: form.phone,
    managerId: form.managerId || '0',
    englishName: form.englishName,
    hireDate: form.hireDate,
    remark: form.remark,
  }
}

// 请求体集中构造并由契约测试锁定字段，防止新增接口误带编辑字段。
export function createEmployeeBody(form: EmployeeFormInput) {
  return {
    ...employeeSharedBody(form),
    username: form.username,
    initialPassword: form.initialPassword,
  }
}

export function updateEmployeeBody(form: EmployeeFormInput) {
  return {
    ...employeeSharedBody(form),
    leaveDate: form.leaveDate,
    expectedVersion: form.version,
  }
}

export interface DepartmentFormInput {
  code: string
  name: string
  parentId: string
  sortOrder: number
  leaderEmployeeId: string
  status: string
  version: number
}

function departmentSharedBody(form: DepartmentFormInput) {
  return {
    code: form.code,
    name: form.name,
    parentId: form.parentId || '0',
    sortOrder: form.sortOrder,
    leaderEmployeeId: form.leaderEmployeeId || '0',
  }
}

export function createDepartmentBody(form: DepartmentFormInput) {
  return departmentSharedBody(form)
}

export function updateDepartmentBody(form: DepartmentFormInput) {
  return {
    ...departmentSharedBody(form),
    status: form.status,
    expectedVersion: form.version,
  }
}
