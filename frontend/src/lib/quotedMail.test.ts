import { describe, expect, test } from 'vitest'
import { attributionLine, escapeText, quotedBlock, stripStylesheets } from './quotedMail'

// 假的 t：把 key 和参数原样拼出来，这样断言看的是「拼进去的是什么」，
// 而不是某个语言此刻的措辞——文案改了不该让这些测试变红。
const t = (key: string, params?: Record<string, unknown>) =>
  params ? `${key}(${Object.entries(params).map(([k, v]) => `${k}=${v}`).join(',')})` : key

const base = { fromEmail: 'lin@buyer.com', fromName: '林采购' }

describe('attributionLine', () => {
  test('带上原信的时间，走 quotedAt 那句', () => {
    const line = attributionLine({ ...base, sentAt: '2026-03-01T08:00:00Z' }, t)
    expect(line).toContain('emails.quotedAt(')
    expect(line).toContain('who=林采购')
    expect(line).toContain('addr=lin@buyer.com')
    // 具体几点取决于跑测试的机器在哪个时区，但一定是个带偏移量的完整时间戳。
    expect(line).toMatch(/when=\d{4}-\d{2}-\d{2} \d{2}:\d{2} [+-]\d{2}:\d{2}/)
  })

  test('没有 sentAt 时用 receivedAt', () => {
    const line = attributionLine({ ...base, receivedAt: '2026-03-01T08:00:00Z' }, t)
    expect(line).toContain('emails.quotedAt(')
  })

  test('sentAt 优先于 receivedAt', () => {
    const both = attributionLine(
      { ...base, sentAt: '2026-03-01T08:00:00Z', receivedAt: '2026-03-02T08:00:00Z' },
      t,
    )
    const sentOnly = attributionLine({ ...base, sentAt: '2026-03-01T08:00:00Z' }, t)
    expect(both).toBe(sentOnly)
  })

  test('一个时间都没有就退回不带日期的那句，不留一个空的「在 ，」', () => {
    const line = attributionLine(base, t)
    expect(line).toBe('林采购 <lin@buyer.com> emails.wrote')
    expect(line).not.toContain('quotedAt')
  })

  test('没有显示名就用地址当名字', () => {
    expect(attributionLine({ fromEmail: 'lin@buyer.com' }, t)).toBe(
      'lin@buyer.com <lin@buyer.com> emails.wrote',
    )
  })
})

describe('quotedBlock', () => {
  test('署名整行转义：地址的尖括号是文字，不是标签', () => {
    const html = quotedBlock({ ...base, bodyHtml: '<p>hi</p>' }, t)
    expect(html).toContain('&lt;lin@buyer.com&gt;')
    expect(html).not.toContain('<lin@buyer.com>')
  })

  test('发件人名字里的标记进不了文档', () => {
    const html = quotedBlock(
      { fromEmail: 'x@y.com', fromName: '<img src=x onerror=alert(1)>', bodyHtml: '<p>hi</p>' },
      t,
    )
    expect(html).not.toContain('<img')
    expect(html).toContain('&lt;img')
  })

  test('HTML 正文原样带走——它就是这封信本来的样子', () => {
    const html = quotedBlock({ ...base, bodyHtml: '<p><b>价格</b>确认</p>' }, t)
    expect(html).toContain('<blockquote><p><b>价格</b>确认</p></blockquote>')
  })

  test('没有 HTML 时用纯文本，并且转义', () => {
    const html = quotedBlock({ ...base, bodyText: '5 < 6 & 7 > 6' }, t)
    expect(html).toContain('<blockquote><p>5 &lt; 6 &amp; 7 &gt; 6</p></blockquote>')
  })

  test('纯文本里的换行变成 <br>，不然整封信折成一段', () => {
    const html = quotedBlock({ ...base, bodyText: '第一行\n第二行' }, t)
    expect(html).toContain('第一行<br>第二行')
  })

  test('正文两边都空时仍是一个完整的引用块', () => {
    const html = quotedBlock(base, t)
    expect(html).toContain('<blockquote><p></p></blockquote>')
  })
})

describe('escapeText', () => {
  test('先转义再换行，别人写的 <br> 三个字还是三个字', () => {
    expect(escapeText('a<br>b')).toBe('a&lt;br&gt;b')
    expect(escapeText('a\nb')).toBe('a<br>b')
  })

  test('引号也转义——将来这段文字如果落进属性里就不会破框', () => {
    expect(escapeText(`"'`)).toBe('&quot;&#39;')
  })
})

// 发件人的样式表不能进我们的页面。
//
// 真炸过（2026-09-10）：回复一封 Cloudflare 的营销邮件，写信框里发件人那栏
// 空了、正文打字也看不见。原因是那封信的样式表里有一句
// `p,div,h1,h2{color:#fff!important}`，落进我们自己的文档之后，整页每个 div
// 里的字都被刷成白的——字一直在，只是白纸上的白字。
//
// 服务端展示收到的邮件时是**故意**留着 `<style>` 的（readerPolicy），因为一封
// 营销邮件的样子几乎全在那张表里；它敢留是因为那份 HTML 只在沙箱 iframe 里
// 渲染。而引用框不是 iframe（人要能手工删减引用），所以摘除得在这一步做。
describe('stripStylesheets', () => {
  test('整块 <style> 连同里面的 CSS 一起去掉', () => {
    const out = stripStylesheets('<p>hi</p><style>p{color:#fff!important}</style><p>bye</p>')
    expect(out).toBe('<p>hi</p><p>bye</p>')
    expect(out).not.toContain('color')
  })

  test('那句真的把页面刷白的规则活不下来', () => {
    const real =
      '<style type="text/css">@media (prefers-color-scheme:dark){' +
      'p,div,h1,h2{color:#fff!important}}</style><div>正文</div>'
    expect(stripStylesheets(real)).toBe('<div>正文</div>')
  })

  test('好几块都去掉——一封营销邮件带十块是常事', () => {
    const out = stripStylesheets('<style>a{}</style><p>x</p><style>b{}</style><p>y</p>')
    expect(out).toBe('<p>x</p><p>y</p>')
  })

  test('带属性的、大写的一样认得出', () => {
    expect(stripStylesheets('<STYLE TYPE="text/css">a{}</STYLE>x')).toBe('x')
  })

  test('行内 style 属性留着——那才是一封信大部分的样子', () => {
    const html = '<td style="padding:20px;background:#f77720"><b>报价</b></td>'
    expect(stripStylesheets(html)).toBe(html)
  })

  test('没有样式表就一个字都不动', () => {
    const html = '<p>普通的一封信</p><blockquote><p>更早的一封</p></blockquote>'
    expect(stripStylesheets(html)).toBe(html)
  })

  test('被截断的 <style> 一路删到底，不把 CSS 当正文留下', () => {
    expect(stripStylesheets('<p>hi</p><style>p{color:#f')).toBe('<p>hi</p>')
  })

  test('style 这三个字母开头的别的东西不受牵连', () => {
    const html = '<p>styled text</p>'
    expect(stripStylesheets(html)).toBe(html)
  })
})

describe('quotedBlock 不把发件人的样式表带进来', () => {
  test('原信里的 <style> 不出现在引用块里', () => {
    const html = quotedBlock(
      { ...base, bodyHtml: '<style>div{color:#fff!important}</style><p>报价确认</p>' },
      t,
    )
    expect(html).not.toContain('<style')
    expect(html).not.toContain('!important')
    expect(html).toContain('<p>报价确认</p>')
  })
})
