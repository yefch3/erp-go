import { checkPassword } from './passwordPolicy'

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
  // 负责哪个国家的市场，两位国家码；空 = 没填。员工邮箱监管的树按它分组。
  countryCode: string
  version: number
  username: string
  initialPassword: string
}

export type EmployeeValidationError =
  | 'required'
  | 'emailInvalid'
  | 'phoneInvalid'
  | 'accountIncomplete'
  // 密码那几条各报各的：说「密码不合要求」等于把人推回去继续猜。
  | 'password_tooShort'
  | 'password_tooLong'
  | 'password_tooCommon'
  | 'password_tooSimple'
  | 'password_fromIdentity'
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
    // 和服务端同一套规则（lib/passwordPolicy），不再只数长度：从前这里写的是
    // 「少于 10 个字符」，于是中文密码在这一关就被拦下（服务端一个汉字算两位，
    // 五个字就够），而常见密码、键盘顺序那几条这里一条都不管，人要提交了才
    // 知道。两边报的是同一条规则，措辞也就能对得上。
    if (form.initialPassword) {
      const problem = checkPassword(form.initialPassword, [
        form.name, form.code, form.username, form.email,
      ])
      if (problem) return `password_${problem}` as EmployeeValidationError
    }
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
    countryCode: form.countryCode,
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
