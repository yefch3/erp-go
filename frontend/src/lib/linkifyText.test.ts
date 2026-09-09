import { describe, expect, test } from 'vitest'
import { linkifyText } from './linkifyText'

describe('纯文本里的网址变成能点的链接', () => {
  // 用户报的那一封：面试通知里的 Zoom 地址，以前只是一行字。
  test('正文里的网址变成锚点', () => {
    const out = linkifyText('Zoom: https://amplitude.zoom.us/j/93698437696')
    expect(out).toContain('<a href="https://amplitude.zoom.us/j/93698437696"')
    expect(out).toContain('target="_blank"')
    expect(out).toContain('rel="noopener noreferrer"')
  })

  test('省了协议的 www 也认，补上 https', () => {
    expect(linkifyText('见 www.example.com')).toContain('href="https://www.example.com"')
  })

  test('邮件地址变成 mailto', () => {
    expect(linkifyText('回信给 sales@buyer.com')).toContain('href="mailto:sales@buyer.com"')
  })
})

describe('先转义再认链接——顺序反了就是个注入口子', () => {
  test('正文里的标签被转义，不会变成真标签', () => {
    const out = linkifyText('<script>alert(1)</script>')
    expect(out).not.toContain('<script>')
    expect(out).toContain('&lt;script&gt;')
  })

  test('javascript: 不认，绝不给它做成可点的', () => {
    const out = linkifyText('javascript:alert(document.cookie)')
    expect(out).not.toContain('<a')
  })

  test('data: 也不认', () => {
    expect(linkifyText('data:text/html,<h1>x</h1>')).not.toContain('<a href="data:')
  })

  // 引号如果不转义，就能从 href 里逃出来自己加属性。
  test('地址里的引号逃不出属性', () => {
    const out = linkifyText('https://x.com/"onmouseover="alert(1)')
    expect(out).not.toContain('onmouseover="alert(1)"')
    expect(out).toContain('&quot;')
  })

  test('查询串里的 & 转义后仍然是同一个地址', () => {
    const out = linkifyText('https://x.com/?a=1&b=2')
    // HTML 属性里的 &amp; 浏览器会解回 &，所以这是对的写法
    expect(out).toContain('href="https://x.com/?a=1&amp;b=2"')
  })
})

describe('末尾的标点不算地址的一部分', () => {
  test('中文句号不吞进去', () => {
    const out = linkifyText('详见 https://x.com/a。')
    expect(out).toContain('href="https://x.com/a"')
    expect(out).toContain('</a>。')
  })

  test('英文句号和逗号同理', () => {
    expect(linkifyText('见 https://x.com/a.')).toContain('href="https://x.com/a"')
    expect(linkifyText('见 https://x.com/a, 然后')).toContain('href="https://x.com/a"')
  })

  // 地址本身带括号的（维基百科那种），括号配对时要留着。
  test('配对的括号是地址的一部分', () => {
    const out = linkifyText('https://en.wikipedia.org/wiki/Go_(programming_language)')
    expect(out).toContain('href="https://en.wikipedia.org/wiki/Go_(programming_language)"')
  })

  test('不配对的右括号是句子的，不是地址的', () => {
    const out = linkifyText('（见 https://x.com/a）')
    expect(out).toContain('href="https://x.com/a"')
    expect(out).not.toContain('href="https://x.com/a）"')
  })
})

describe('其余的照旧', () => {
  test('没有网址时只做转义，正文一个字不动', () => {
    expect(linkifyText('你好，世界')).toBe('你好，世界')
  })

  test('空正文不报错', () => {
    expect(linkifyText('')).toBe('')
  })

  test('一行里两个地址各自成链接', () => {
    const out = linkifyText('https://a.com 和 https://b.com')
    expect(out.match(/<a /g)).toHaveLength(2)
  })

  test('换行和空格保持原样（外面是 pre）', () => {
    expect(linkifyText('第一行\n  第二行')).toBe('第一行\n  第二行')
  })
})
