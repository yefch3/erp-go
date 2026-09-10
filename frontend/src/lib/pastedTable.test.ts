import { describe, expect, it } from 'vitest'
import { imageWidth, looksLikeTable, MAIL_BODY_WIDTH, tableFromClipboard } from './pastedTable'

// Excel 复制一块 2×3 区域时剪贴板里的样子（简化）。
const EXCEL_HTML = '<html><body><table border=0><tr><td>x</td></tr></table></body></html>'

describe('tableFromClipboard', () => {
  it('认出表格，重建成 thead + tbody', () => {
    const out = tableFromClipboard(EXCEL_HTML, '品名\t数量\n钢卷\t100\n钢管\t250')
    expect(out).toContain('<thead><tr><th')
    expect(out).toContain('品名')
    expect(out).toContain('<tbody><tr><td')
    expect(out).toContain('钢卷')
    // 三行进去，表头一行 + 正文两行出来
    expect(out?.match(/<tr>/g)).toHaveLength(3)
  })

  it('外来 HTML 一个字节都不进结果', () => {
    // 这是这个模块最要紧的一条：那份 text/html 只用来回答「是不是表格」，
    // 它的内容永远不会出现在插入的东西里。
    const out = tableFromClipboard(
      '<table class="MsoNormalTable" style="mso-x:1"><script>alert(1)</script></table>',
      'a\tb\nc\td',
    )
    expect(out).not.toContain('script')
    expect(out).not.toContain('Mso')
    expect(out).not.toContain('mso-')
  })

  it('单元格内容会转义', () => {
    const out = tableFromClipboard(EXCEL_HTML, 'a\tb\n<img onerror=x>\t&amp;')
    expect(out).toContain('&lt;img onerror=x&gt;')
    expect(out).not.toContain('<img')
    // 原文里的 &amp; 是五个字面字符，要再转义一层才不会在页面上变成 &
    expect(out).toContain('&amp;amp;')
  })

  it('剪贴板里没有 <table> 就不管——那不是表格', () => {
    // 一段带制表符的代码、一份对齐过的清单都长这样。变成表格比不变更糟。
    expect(tableFromClipboard('<p>hello</p>', 'a\tb\nc\td')).toBeNull()
    expect(tableFromClipboard('', 'a\tb\nc\td')).toBeNull()
  })

  it('列数不齐就不管', () => {
    // 这是防误判的另一半：真表格每行列数一定相同。
    expect(tableFromClipboard(EXCEL_HTML, 'a\tb\nc\td\te')).toBeNull()
  })

  it('只有一行、或者只有一列，都不算表格', () => {
    expect(tableFromClipboard(EXCEL_HTML, 'a\tb')).toBeNull()
    expect(tableFromClipboard(EXCEL_HTML, 'a\nb\nc')).toBeNull()
  })

  it('Windows 的 \\r\\n 和结尾多出来的换行都吃得下', () => {
    const out = tableFromClipboard(EXCEL_HTML, '品名\t数量\r\n钢卷\t100\r\n')
    expect(out?.match(/<tr>/g)).toHaveLength(2)
  })

  it('Excel 有时每行末尾多一个制表符，不该多出一空列', () => {
    const out = tableFromClipboard(EXCEL_HTML, 'a\tb\t\nc\td\t')
    // `<th ` 带空格：不带的话 `<thead>` 也会被数进去。
    expect(out?.match(/<th /g)).toHaveLength(2)
    expect(out?.match(/<td /g)).toHaveLength(2)
  })

  it('两列的表格第二列整列是空的，不能被削成一列', () => {
    // 上面那条「去掉多余空列」的反面。只有原本三列以上才削，否则一个
    // 「品名 / 备注（都没填）」的表格会变成一列。
    const out = tableFromClipboard(EXCEL_HTML, '品名\t备注\n钢卷\t\n钢管\t')
    expect(out?.match(/<th /g)).toHaveLength(2)
  })

  it('单元格里本来的前后空格是内容，不 trim', () => {
    const out = tableFromClipboard(EXCEL_HTML, ' a \tb\nc\td')
    expect(out).toContain('> a <')
  })

  it('中间的空行是表格里的一个空行，不是分隔符', () => {
    const out = tableFromClipboard(EXCEL_HTML, 'a\tb\n\t\nc\td')
    expect(out?.match(/<tr>/g)).toHaveLength(3)
  })
})

describe('imageWidth', () => {
  it('比正文窄的图用它自己的宽度', () => {
    // 从前这里写死 160，这就是 issue #363 里「表格很小」的直接原因。
    expect(imageWidth(320)).toBe(320)
  })

  it('比正文宽的收到正文宽度', () => {
    expect(imageWidth(1600)).toBe(MAIL_BODY_WIDTH)
  })

  it('量不到尺寸时回 0，让调用方干脆不写 width', () => {
    // 不写比写一个猜的数好：浏览器会用图片真实尺寸，max-width:100% 兜上限。
    expect(imageWidth(0)).toBe(0)
    expect(imageWidth(NaN)).toBe(0)
    expect(imageWidth(-5)).toBe(0)
  })
})

describe('looksLikeTable', () => {
  it('和 tableFromClipboard 同一套判断——只是不产出，只回是不是', () => {
    // 分开两个函数是为了「不成立就一次网络都不发」：粘贴是高频动作，
    // 不该每次都往返一趟服务端。判断口径必须和重建那条完全一致，否则会
    // 出现「说是表格、结果重建不出来」的空档。
    expect(looksLikeTable(EXCEL_HTML, '品名\t数量\n钢卷\t100')).toBe(true)
    expect(looksLikeTable('<p>一段话</p>', 'a\tb\nc\td')).toBe(false)
    expect(looksLikeTable(EXCEL_HTML, 'a\tb\nc\td\te')).toBe(false)
  })
})
