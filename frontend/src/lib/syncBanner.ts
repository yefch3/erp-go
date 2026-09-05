/**
 * 同步出错时，页面顶部那条横幅该长什么样。
 *
 * 从前只要有错误文本就显示「重新登录邮箱」按钮。可 263 隔几分钟就掐一次
 * 空闲连接，每掐一次员工就被劝去重输一遍授权码——重输从来没修好过任何东西，
 * 下一次同步碰巧成功只是因为超时是间歇的。「一直要重新登录」就是这么来的。
 *
 * 现在靠后端给的 needsReauth 判断：只有授权码真被拒时才给那颗按钮；其余的
 * 说清楚是服务器暂时连不上、会自动重试，不给按钮。
 */
export interface SyncBanner {
  /** 显示给人看的错误文本；空 = 不显示横幅。 */
  text: string
  /** 要不要那颗「重新登录邮箱」按钮。 */
  offerReauth: boolean
}

export function syncBanner(input: { detail?: string; lastError?: string; needsReauth?: boolean }): SyncBanner {
  const text = (input.detail || input.lastError || '').trim()
  if (!text) return { text: '', offerReauth: false }
  return { text, offerReauth: input.needsReauth === true }
}
