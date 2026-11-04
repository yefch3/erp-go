import { beforeEach, describe, expect, it, vi } from 'vitest'
import { createPinia, setActivePinia } from 'pinia'

// 接线测试：mailUnlockOwner.test.ts 证明了 keepTokensFor / claimUnstampedTokens
// 本身是对的，这里证明 ERP 登录和身份刷新**真的调用了它们**。少了这一层，
// 有人在重构 login() 时删掉那一行，所有测试照样全绿——而 ERP 登录过期已经
// 不再清邮箱令牌，上一个人的令牌就会留给下一个登录的人。

const post = vi.fn()
const get = vi.fn()
vi.mock('../api', () => ({
  post: (...args: unknown[]) => post(...args),
  get: (...args: unknown[]) => get(...args),
  quietErrors: {},
}))

class MemoryStorage {
  private m = new Map<string, string>()
  getItem(k: string) {
    return this.m.has(k) ? (this.m.get(k) as string) : null
  }
  setItem(k: string, v: string) {
    this.m.set(k, String(v))
  }
  removeItem(k: string) {
    this.m.delete(k)
  }
  clear() {
    this.m.clear()
  }
}
;(globalThis as { localStorage?: unknown }).localStorage = new MemoryStorage()

const { useAuthStore } = await import('./auth')

function loginResponse(id: string) {
  return {
    accessToken: '',
    expiresInSeconds: '0',
    employee: { id, name: `员工${id}`, email: '', departmentName: '浙江' },
    permissionCodes: ['mail:email:read'],
    tenantId: '4',
  }
}

// 某个人验过邮箱之后的浏览器：令牌记在他名下。
function tokensVerifiedBy(employeeId: string) {
  localStorage.setItem('mailUnlock', `tok-${employeeId}`)
  localStorage.setItem('mailUnlockTokens', JSON.stringify({ 58: `tok-${employeeId}` }))
  localStorage.setItem('mailUnlockOwner', employeeId)
}

beforeEach(() => {
  localStorage.clear()
  post.mockReset()
  get.mockReset()
  setActivePinia(createPinia())
})

describe('ERP 登录决定邮箱令牌的去留', () => {
  it('同一个人重新登录，令牌原样留着', async () => {
    tokensVerifiedBy('17')
    post.mockResolvedValue(loginResponse('17'))
    await useAuthStore().login('a', 'x')
    expect(localStorage.getItem('mailUnlock')).toBe('tok-17')
    expect(localStorage.getItem('mailUnlockTokens')).toContain('tok-17')
  })

  it('换了一个人登录，上一个人的令牌当场清掉', async () => {
    tokensVerifiedBy('17')
    post.mockResolvedValue(loginResponse('25'))
    await useAuthStore().login('b', 'x')
    expect(localStorage.getItem('mailUnlock')).toBeNull()
    expect(localStorage.getItem('mailUnlockTokens')).toBeNull()
    expect(localStorage.getItem('mailUnlockOwner')).toBe('25')
  })

  it('会话确认身份后，没记名的令牌记到这个人名下', async () => {
    localStorage.setItem('employeeId', '17')
    localStorage.setItem('employeeName', '员工17')
    localStorage.setItem('mailUnlock', 'legacy')
    get.mockResolvedValue({ profile: { id: '17', tenantId: '4', name: '员工17', email: '', departmentName: '浙江' } })
    await useAuthStore().refreshProfile()
    expect(localStorage.getItem('mailUnlockOwner')).toBe('17')
  })
})
