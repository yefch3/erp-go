import { describe, expect, it, vi } from 'vitest'

// 被测模块从 ../api 拿 axios 实例和 saveBlob；api.ts 一加载就带起 i18n、router 和
// localStorage，node 里没有这些。每个测试都注入自己的 transport，真实例用不到。
vi.mock('../api', () => ({ http: { put: vi.fn(), get: vi.fn() }, saveBlob: vi.fn() }))

import {
  RELAY_DOWNLOAD_TIMEOUT_MS,
  RELAY_HEADER,
  RELAY_PATH,
  RELAY_UPLOAD_TIMEOUT_MS,
  fetchViaRelay,
  relayFailureReason,
  saveViaRelay,
  uploadViaRelay,
  type RelayTransport,
} from './storageRelay'

// 这里只碰 file.type，其余原样交给中转。用最小替身而不是真 File：单测跑在
// node 里，为一个只读 type 的函数拉起 jsdom 不值得。
const aFile = (type = 'image/jpeg') => ({ type, name: '照片.jpg' }) as unknown as Blob

const signed = 'http://localhost:19000/erp-files/quality/4/1/1762-a.jpg?X-Amz-Credential=x&X-Amz-Signature=y'

function transport(overrides: Partial<RelayTransport> = {}) {
  const put = vi.fn(overrides.put ?? (async () => ({})))
  const get = vi.fn(overrides.get ?? (async () => ({ data: { size: 3 } as unknown as Blob })))
  const t: RelayTransport = { put, get }
  return { put, get, t }
}

describe('uploadViaRelay', () => {
  it('把文件和签名地址一起交给网关，而不是自己连存储', async () => {
    const { put, t } = transport()
    const file = aFile()

    await uploadViaRelay(signed, file, t)

    expect(put).toHaveBeenCalledTimes(1)
    const [url, body, cfg] = put.mock.calls[0] as unknown as [string, Blob, Record<string, unknown>]
    expect(url).toBe(RELAY_PATH)
    expect(body).toBe(file)
    expect(cfg.headers).toEqual({ [RELAY_HEADER]: signed, 'Content-Type': 'image/jpeg' })
    // 跨境传大文件，默认 15 秒不够；失败要连着文件名一起说，所以不用通用 toast。
    expect(cfg.timeout).toBe(RELAY_UPLOAD_TIMEOUT_MS)
    expect(cfg.quiet).toBe(true)
  })

  it('浏览器认不出类型的文件按二进制流送', async () => {
    const { put, t } = transport()
    await uploadViaRelay(signed, aFile(''), t)
    const cfg = (put.mock.calls[0] as unknown as [string, Blob, { headers: Record<string, string> }])[2]
    expect(cfg.headers['Content-Type']).toBe('application/octet-stream')
  })

  it('没有上传地址就不发请求', async () => {
    const { put, t } = transport()
    await expect(uploadViaRelay('', aFile(), t)).rejects.toThrow('缺少上传地址')
    expect(put).not.toHaveBeenCalled()
  })

  it('网关的拒绝原样往上冒，由调用方决定怎么说', async () => {
    const refusal = { success: false, code: 'STORAGE_RELAY_REJECTED', message: '文件存储拒绝了这次上传（HTTP 403）' }
    const { t } = transport({ put: vi.fn(async () => Promise.reject(refusal)) })
    await expect(uploadViaRelay(signed, aFile(), t)).rejects.toBe(refusal)
  })
})

describe('fetchViaRelay / saveViaRelay', () => {
  it('经网关取文件：带签名地址、按 blob 收、预算放宽', async () => {
    const blob = { size: 3 } as unknown as Blob
    const { get, t } = transport({ get: vi.fn(async () => ({ data: blob })) })

    const got = await fetchViaRelay(signed, t)

    expect(got).toBe(blob)
    const [url, cfg] = get.mock.calls[0] as unknown as [string, Record<string, unknown>]
    expect(url).toBe(RELAY_PATH)
    expect(cfg.headers).toEqual({ [RELAY_HEADER]: signed })
    expect(cfg.responseType).toBe('blob')
    expect(cfg.timeout).toBe(RELAY_DOWNLOAD_TIMEOUT_MS)
    expect(cfg.quiet).toBe(true)
  })

  it('没有地址就不发请求', async () => {
    const { get, t } = transport()
    await expect(fetchViaRelay('', t)).rejects.toThrow('缺少文件地址')
    expect(get).not.toHaveBeenCalled()
  })

  it('取回来交给保存函数，用页面知道的文件名', async () => {
    const blob = { size: 3 } as unknown as Blob
    const { t } = transport({ get: vi.fn(async () => ({ data: blob })) })
    const save = vi.fn()

    await saveViaRelay(signed, '发票.pdf', t, save)
    expect(save).toHaveBeenCalledWith(blob, '发票.pdf')

    await saveViaRelay(signed, '', t, save)
    expect(save).toHaveBeenLastCalledWith(blob, '文件')
  })
})

describe('relayFailureReason', () => {
  it('服务端信封、Error、垃圾各取所能', () => {
    expect(relayFailureReason({ success: false, code: 'X', message: ' 服务器连不上文件存储 ' })).toBe('服务器连不上文件存储')
    expect(relayFailureReason(new Error('Network Error'))).toBe('Network Error')
    expect(relayFailureReason({ message: 42 })).toBe('')
    expect(relayFailureReason(null)).toBe('')
    expect(relayFailureReason('boom')).toBe('')
  })
})
