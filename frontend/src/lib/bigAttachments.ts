// 哪几个附件会变成下载链接。
//
// 和服务端 biglinks.go 的 splitCarriedAndLinked 是**同一条规则**，各写一遍
// 是有意的：服务端那份是真正执行的，这份只为在写信框里标出来。放弃「让服务端
// 算一遍再回给前端」是因为那会让每加一个附件都往返一次，而这段判断只依赖
// 文件大小——没有任何服务端才知道的信息。
//
// 两份必须同时改。规则本身只有一句话，两边的测试也钉的是同一组例子。
//
// 为什么要在发之前标出来：收件人拿到的东西变了——不是附件而是一个链接，有些
// 企业安全网关会拦外链，文件也存在我们服务器上。Outlook 和 Gmail 同样会当面
// 说清楚（「此文件将通过 OneDrive 共享」），不是悄悄换掉。

/** 和服务端 MaxCarriedAttachmentBytes 一致。 */
export const MAX_CARRIED_ATTACHMENT_BYTES = 15 * 1024 * 1024

export interface SizedFile {
  size: number
}

/**
 * 返回每个附件会不会变成链接，顺序和传进来的一致。
 *
 * 规则：装得下就全带；装不下就从**大的**开始转，直到剩下的装得下。
 */
export function linkedAttachmentFlags(
  files: readonly SizedFile[],
  limit = MAX_CARRIED_ATTACHMENT_BYTES,
): boolean[] {
  const flags = files.map(() => false)
  let total = files.reduce((n, f) => n + (f.size || 0), 0)
  if (total <= limit) return flags

  // 从大到小，同样大的按原顺序——排序要稳定，否则同样两个文件今天标这个、
  // 明天标那个。Array.prototype.sort 在现代引擎里是稳定的，但这里显式按
  // 下标兜底，不依赖那个保证。
  const order = files.map((_, i) => i)
  order.sort((a, b) => (files[b].size || 0) - (files[a].size || 0) || a - b)

  for (const i of order) {
    if (total <= limit) break
    flags[i] = true
    total -= files[i].size || 0
  }
  return flags
}
