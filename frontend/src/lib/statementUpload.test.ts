import { describe, expect, it, vi } from 'vitest'

import { uploadBankStatement, type StatementUploadDeps } from './statementUpload'

// 这个函数只碰 file.name，其余原样交给直传。用最小替身而不是真 File：
// 单测跑在 node 环境里，没有 File 这个全局，而为一个只读 name 的函数拉起
// jsdom 是本末倒置。
const aFile = () => ({ name: 'august.pdf' }) as unknown as File

// post 写成普通函数而不是 vi.fn：它是泛型的，被 vi.fn 包一层之后返回类型
// 塌成 unknown，vue-tsc 会红。要断言调用就自己记 calls，够用。
function deps(overrides: Partial<StatementUploadDeps> = {}) {
  const calls: Array<{ url: string; body?: object }> = []
  const post = async <T>(url: string, body?: object): Promise<T> => {
    calls.push({ url, body })
    if (url.endsWith('/presign')) {
      return { key: 'bank-transactions/1/42/august.pdf', uploadUrl: 'https://store/put?sig=x' } as T
    }
    return {} as T
  }
  const d: StatementUploadDeps = {
    post, put: async () => ({ ok: true, status: 200 }), ...overrides,
  }
  return { calls, deps: d }
}

describe('uploadBankStatement', () => {
  it('三步都走：签地址 → 直传 → 登记 key', async () => {
    const { calls, deps: d } = deps()
    const put = vi.fn(async () => ({ ok: true, status: 200 }))
    const file = aFile()
    const key = await uploadBankStatement('42', file, { ...d, put })

    expect(calls.map((c) => c.url)).toEqual([
      '/bank-transactions/42/attachment/presign',
      '/bank-transactions/42/attachment',
    ])
    expect(calls[0].body).toEqual({ fileName: 'august.pdf' })
    expect(put).toHaveBeenCalledWith('https://store/put?sig=x', file)
    // 登记的 key 必须是签地址时给的那一个，不能自己拼一个。
    expect(calls[1].body).toEqual({ key: 'bank-transactions/1/42/august.pdf' })
    expect(key).toBe('bank-transactions/1/42/august.pdf')
  })

  // fetch 对 4xx/5xx 不 reject，只把 ok 置 false。不显式检查的话，一个 403
  // 的直传会一路走到登记那一步，把一个根本不存在的 key 挂到流水行上——
  // 列表里于是出现一个点开是 404 的附件链接，而当时页面显示的是「已上传」。
  it('直传返回 4xx 时停在第二步，不去登记一个不存在的 key', async () => {
    const { calls, deps: d } = deps()
    await expect(
      uploadBankStatement('42', aFile(), { ...d, put: async () => ({ ok: false, status: 403 }) }),
    ).rejects.toThrow(/403/)
    expect(calls.map((c) => c.url)).toEqual(['/bank-transactions/42/attachment/presign'])
  })

  it('签地址失败就整个失败，不吞', async () => {
    const post = async <T>(): Promise<T> => {
      throw new Error('boom')
    }
    await expect(uploadBankStatement('42', aFile(), { post })).rejects.toThrow('boom')
  })

  // 登记流水那条路是「先建行、再传纸」。行没建成却调上传，早点炸出来比
  // 签一个 /bank-transactions/0/... 的地址强——后者会一路 404 到用户脸上。
  it('没有流水行 id 时直接拒绝', async () => {
    const { deps: d } = deps()
    await expect(uploadBankStatement('', aFile(), d)).rejects.toThrow(/流水行 id/)
    await expect(uploadBankStatement('0', aFile(), d)).rejects.toThrow(/流水行 id/)
  })
})
