import { describe, expect, it } from 'vitest'
import {
  createDepartmentBody,
  createEmployeeBody,
  updateDepartmentBody,
  updateEmployeeBody,
  validateEmployeeForm,
  type DepartmentFormInput,
  type EmployeeFormInput,
} from './iamForms'

const employee: EmployeeFormInput = {
  id: '9', code: 'E009', name: '测试员工', departmentId: '2', position: '会计',
  email: 'staff@example.com', phone: '+1 (212) 555-0100', managerId: '1',
  englishName: 'Tester', hireDate: '2026-08-01', leaveDate: '2026-08-31', remark: '备注',
  version: 3, username: 'tester', initialPassword: 'password-10',
}

const department: DepartmentFormInput = {
  code: 'FIN', name: '财务部', parentId: '1', sortOrder: 10,
  leaderEmployeeId: '2', status: 'ACTIVE', version: 4,
}

describe('IAM 请求字段契约', () => {
  it('新增员工不携带编辑专用字段', () => {
    expect(Object.keys(createEmployeeBody(employee))).toEqual([
      'code', 'name', 'departmentId', 'position', 'email', 'phone', 'managerId',
      'englishName', 'hireDate', 'remark', 'username', 'initialPassword',
    ])
  })

  it('编辑员工不携带账号创建字段', () => {
    expect(Object.keys(updateEmployeeBody(employee))).toEqual([
      'code', 'name', 'departmentId', 'position', 'email', 'phone', 'managerId',
      'englishName', 'hireDate', 'remark', 'leaveDate', 'expectedVersion',
    ])
  })

  it('新增部门不携带状态和版本号', () => {
    expect(Object.keys(createDepartmentBody(department))).toEqual([
      'code', 'name', 'parentId', 'sortOrder', 'leaderEmployeeId',
    ])
  })

  it('编辑部门包含状态和并发版本号', () => {
    expect(Object.keys(updateDepartmentBody(department))).toEqual([
      'code', 'name', 'parentId', 'sortOrder', 'leaderEmployeeId', 'status', 'expectedVersion',
    ])
  })
})

describe('员工表单校验', () => {
  it('拒绝错误邮箱、电话和过短密码', () => {
    expect(validateEmployeeForm({ ...employee, email: 'bad-email' }, false)).toBe('emailInvalid')
    expect(validateEmployeeForm({ ...employee, phone: 'call-me' }, false)).toBe('phoneInvalid')
    expect(validateEmployeeForm({ ...employee, initialPassword: 'short' }, false)).toBe('passwordTooShort')
  })

  it('拒绝早于入职日期的离职日期', () => {
    expect(validateEmployeeForm({ ...employee, leaveDate: '2026-07-31' }, true)).toBe('leaveBeforeHire')
  })

  it('接受合法的新增和编辑资料', () => {
    expect(validateEmployeeForm(employee, false)).toBeNull()
    expect(validateEmployeeForm(employee, true)).toBeNull()
  })
})
