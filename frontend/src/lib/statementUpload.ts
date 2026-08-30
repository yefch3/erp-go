// 银行对账单的直传三步。
//
// 文件**不经过我们的服务**：先要一个短命的直传地址，浏览器直接把 PDF 传给
// 对象存储，传完才回来登记 key。几十兆的全月流水也不会把网关撑爆。
//
// 抽成一个函数，是因为它有两个调用方（登记流水时随手带的、和列表里事后补
// 的），而这三步漏掉任何一步都**不报错**——只是那张纸悄悄没上去，页面还
// 显示成功。两份实现迟早会走偏，而走偏的那一份不会有人发现。
//
// 尤其是第二步：`fetch` 对 4xx/5xx **不会 reject**，它只是把 ok 置为 false。
// 不显式检查的话，一个 403 的直传会一路走到第三步，把一个根本不存在的
// object key 登记到流水行上——列表里出现一个点开是 404 的附件链接。

export interface PresignedUpload {
  key: string
  uploadUrl: string
}

export interface StatementUploadDeps {
  /** POST 到网关，返回解包后的响应体。 */
  post: <T>(url: string, body?: object) => Promise<T>
  /** 直传用的 fetch，默认用全局那个。 */
  put?: (url: string, file: File) => Promise<{ ok: boolean; status: number }>
}

/**
 * 把一份对账单挂到一条银行流水上。任一步失败都抛错，调用方决定怎么说。
 *
 * txnID 必须是**已经落库**的流水行 id：直传地址是按行签的（key 前缀里带着
 * 租户和行号，那也是这套存储里唯一的跨租户隔离），没有行就签不出地址。
 */
export async function uploadBankStatement(
  txnID: string,
  file: File,
  deps: StatementUploadDeps,
): Promise<string> {
  if (!txnID || txnID === '0') {
    throw new Error('uploadBankStatement: 没有流水行 id，直传地址签不出来')
  }
  const signed = await deps.post<PresignedUpload>(
    `/bank-transactions/${txnID}/attachment/presign`,
    { fileName: file.name },
  )
  const put = deps.put ?? defaultPut
  const res = await put(signed.uploadUrl, file)
  if (!res.ok) {
    throw new Error(`uploadBankStatement: 直传失败 ${res.status}`)
  }
  await deps.post(`/bank-transactions/${txnID}/attachment`, { key: signed.key })
  return signed.key
}

async function defaultPut(url: string, file: File) {
  const res = await fetch(url, { method: 'PUT', body: file })
  return { ok: res.ok, status: res.status }
}
