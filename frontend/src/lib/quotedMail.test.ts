import { describe, expect, test } from 'vitest'
import { attributionLine, escapeText, quotedBlock } from './quotedMail'

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
