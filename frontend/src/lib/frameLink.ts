// frame 里那些链接，点了到底该去哪儿。
//
// 正文渲染在一个 sandbox 的 iframe 里。理论上 `allow-popups` 加文档里的
// <base target="_blank"> 就够让链接自己打开了——但线上就是点不动，而这类
// 行为在不同浏览器、不同 sandbox 组合下的差别很难在本地复现，更难保证以后
// 不再变。
//
// 所以不赌它：frame 里的点击一律拦下来，由**父窗口**去开。父窗口没有任何
// sandbox 限制，这条路是确定的。
//
// 顺带得到一层实打实的好处：协议白名单挪到了「点击那一刻」。净化器已经过滤
// 过一遍，但净化和开链接是两件事——净化管的是存进来的 HTML，这里管的是
// 「我们要不要替使用者去打开这个地址」。两道都设，谁也不替谁。

/** 允许打开的协议。其余一律不开，包括 javascript: 和 data:。 */
const SAFE = /^(?:https?|mailto):$/i

/**
 * 这个 href 能不能替使用者打开。能就返回规范化之后的地址，不能返回 null。
 *
 * 用 URL 解析而不是字符串前缀匹配：`java\tscript:alert(1)`、
 * ` JavaScript:…`、大小写混写这些都能骗过前缀匹配，而解析器会把它们
 * 归到真正的协议上去。
 */
export function safeExternalHref(raw: string | null | undefined): string | null {
  if (!raw) return null
  let u: URL
  try {
    // base 只为解析相对地址用得上；邮件里的相对地址没有意义，解析出来
    // 会落到 about: 上，下面的白名单自然会拒。
    u = new URL(raw, 'about:blank')
  } catch {
    return null
  }
  if (!SAFE.test(u.protocol)) return null
  return u.href
}
