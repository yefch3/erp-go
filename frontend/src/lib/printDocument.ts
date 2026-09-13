// 打印一份 HTML：走我们自己的一个隐藏 frame，不开新标签页。
//
// await 之后再 window.open 正是弹窗拦截器要拦的东西，而且拦得悄无声息——
// 点了"没反应"。frame 永远放行。它还把发信人的文档关在一个 frame 里，不让
// 它成为我们域名下的一个顶层页面；这里的 HTML 是我们自己拼的对话记录，这一点
// 没那么要紧，但守着它不花什么。
//
// 抽出来是因为现在有两处要打印：邮件页的阅读区，和双击弹出的单独窗口。
export function printDocument(html: string): void {
  const frame = document.createElement('iframe')
  frame.setAttribute('aria-hidden', 'true')
  // 藏起来，但要排版。display:none 的 frame 不排版，打印出来是白纸。
  frame.style.cssText = 'position:absolute;width:0;height:0;border:0;visibility:hidden;'
  frame.srcdoc = html
  frame.onload = () => {
    const win = frame.contentWindow
    if (!win) {
      frame.remove()
      return
    }
    win.focus()
    win.print()
    // 打印对话框还开着就把 frame 拆掉，Safari 会把这次打印作废，所以等它
    // 关了再拆——再加一个定时器兜底，因为不少浏览器根本不发 afterprint。
    const drop = () => frame.remove()
    win.addEventListener('afterprint', drop, { once: true })
    setTimeout(drop, 120_000)
  }
  document.body.appendChild(frame)
}
