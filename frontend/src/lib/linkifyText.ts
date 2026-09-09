// 纯文本邮件里的网址，变成能点的链接。
//
// 纯文本的信在界面上是 `{{ body }}` 直接渲染的——一个 Zoom 会议地址、一个
// 退订链接、一个报价单地址，全都是**字面上的文字**：看得见、点不动、只能
// 手工选中复制。而这种信正是最常带链接的那一类（面试通知、系统通知、退订）。
//
// **先转义，再认链接。** 顺序反过来就是个注入口子：先插了 <a> 再转义会把
// 自己的标签也转义掉，先转义再插则保证除了我们自己加的锚点之外，正文里
// 一个标签都不会出现。转义之后 & 变成 &amp;，而 HTML 属性里的 &amp; 浏览器
// 会解回 &，所以在转义后的串上匹配是对的，href 也是对的。
//
// 只认 http/https/www 和邮件地址。javascript: 和 data: 一律不认——那两个
// 恰恰是「把一段文字变成可点的东西」最容易被利用的地方。

const ESCAPES: Record<string, string> = {
  '&': '&amp;',
  '<': '&lt;',
  '>': '&gt;',
  '"': '&quot;',
  "'": '&#39;',
}

export function escapeHtml(s: string): string {
  return s.replace(/[&<>"']/g, (c) => ESCAPES[c])
}

// 在**已经转义过**的文本上匹配。三选一：
//   https?://…      正经网址
//   www.…           省了协议的写法，补一个 https://
//   someone@host.tld 邮件地址 → mailto:
//
// [^\s<]+ 里的 < 排除是多余的保险：这时候正文里已经没有裸的 < 了。
const CANDIDATE = /(https?:\/\/[^\s<]+|www\.[^\s<]+|[^\s<>()[\]{}'",;:@]+@[^\s<>()[\]{}'",;:@]+\.[a-zA-Z]{2,})/g

// 网址后面紧跟的标点不算网址的一部分。
//
// 「详见 https://x.com/a。」这句里的句号是中文句号，不是路径；英文里的
// 逗号、句号、分号、右括号同理。不剥的话链接会带上标点，点开就是 404。
const TRAILING = /[.,;:!?。，；：！？、"'）】》>]+$/

// 右括号单独处理：Wikipedia 那类地址本身就带括号
// （https://en.wikipedia.org/wiki/Go_(programming_language)），只有在括号
// 不配对时才认为它是句子的括号。
function trimTrailing(url: string): { url: string; trailing: string } {
  let out = url
  let cut = ''
  for (;;) {
    const m = out.match(TRAILING)
    if (!m) break
    // 配对的右括号留着
    if (m[0] === ')' && (out.split('(').length - 1) > (out.split(')').length - 1)) break
    cut = m[0] + cut
    out = out.slice(0, -m[0].length)
  }
  // 末尾的 ) 单独再看一次：TRAILING 里没有它，因为多数情况下它属于地址。
  while (out.endsWith(')') && (out.split('(').length - 1) < (out.split(')').length - 1)) {
    cut = ')' + cut
    out = out.slice(0, -1)
  }
  return { url: out, trailing: cut }
}

/**
 * 把纯文本变成可以安全塞进页面的 HTML：先转义，再把网址和邮件地址变成锚点。
 *
 * 返回的串里，除了我们自己加的 <a>，不含任何来自正文的标签。
 */
export function linkifyText(text: string | undefined | null): string {
  if (!text) return ''
  return escapeHtml(text).replace(CANDIDATE, (match) => {
    const { url, trailing } = trimTrailing(match)
    if (!url) return match

    let href: string
    if (url.startsWith('http://') || url.startsWith('https://')) {
      href = url
    } else if (url.startsWith('www.')) {
      href = 'https://' + url
    } else {
      href = 'mailto:' + url
    }
    // noopener/noreferrer：新标签页不该拿到我们这一页的引用，也不该把
    // 我们的地址带给对方——收到的信是别人写的，链接指向哪儿不由我们决定。
    return `<a href="${href}" target="_blank" rel="noopener noreferrer">${url}</a>${trailing}`
  })
}

/**
 * 把纯文本正文包成一份能交给 MailBody 的 HTML。
 *
 * 为什么不是在页面里 v-html：收到的信一律只在沙箱 frame 里渲染，这条规矩由
 * scripts/check-mail-sandbox.sh 守着。纯文本看起来「安全得不需要沙箱」，但
 * 把它单独开一个口子就意味着以后每加一处渲染都要重新判断一次安全性，而那条
 * 守卫存在的原因正是「改了三处、漏了两处，没有任何东西提醒」。
 *
 * 走 frame 还白得一件事：MailBody 的文档里有 <base target="_blank">，链接
 * 自然在新标签页打开，不会把信本身换成目标网页。
 *
 * pre 的样式写在行内：frame 里是另一个文档，外面 .in-text 那条 scoped 规则
 * 到不了这里。
 */
export function plainTextToHtml(text: string | undefined | null): string {
  return '<pre style="white-space:pre-wrap;word-break:break-word;margin:0;' +
    'font-family:inherit;font-size:14px;line-height:1.6">' +
    linkifyText(text) +
    '</pre>'
}
