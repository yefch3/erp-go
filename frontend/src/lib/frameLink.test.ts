import { describe, expect, test } from 'vitest'
import { safeExternalHref } from './frameLink'

describe('替使用者打开一个地址之前，先问它是什么协议', () => {
  test('http 和 https 放行', () => {
    expect(safeExternalHref('https://amplitude.zoom.us/j/936')).toBe('https://amplitude.zoom.us/j/936')
    expect(safeExternalHref('http://example.com/a')).toBe('http://example.com/a')
  })

  test('mailto 放行——信里最常见的第二种链接', () => {
    expect(safeExternalHref('mailto:sales@buyer.com')).toBe('mailto:sales@buyer.com')
  })

  // 下面这些是「把文字变成可点的东西」时最容易被利用的地方。
  test('javascript: 不开', () => {
    expect(safeExternalHref('javascript:alert(1)')).toBeNull()
  })

  test('大小写混写也不开——前缀匹配会漏，解析器不会', () => {
    expect(safeExternalHref('JaVaScRiPt:alert(1)')).toBeNull()
  })

  test('中间塞控制字符也不开', () => {
    expect(safeExternalHref('java\tscript:alert(1)')).toBeNull()
    expect(safeExternalHref('java\nscript:alert(1)')).toBeNull()
  })

  test('前导空白也不开', () => {
    expect(safeExternalHref('  javascript:alert(1)')).toBeNull()
  })

  test('data: 不开', () => {
    expect(safeExternalHref('data:text/html,<h1>x</h1>')).toBeNull()
  })

  test('vbscript: 不开', () => {
    expect(safeExternalHref('vbscript:msgbox(1)')).toBeNull()
  })

  test('file: 不开——不替别人的信去翻本机文件', () => {
    expect(safeExternalHref('file:///etc/passwd')).toBeNull()
  })

  test('空的和解析不了的都不开', () => {
    expect(safeExternalHref('')).toBeNull()
    expect(safeExternalHref(null)).toBeNull()
    expect(safeExternalHref(undefined)).toBeNull()
    expect(safeExternalHref('   ')).toBeNull()
  })

  // 邮件里的相对地址没有意义：它相对的是 about:srcdoc，指不到任何东西。
  test('相对地址不开', () => {
    expect(safeExternalHref('/settings')).toBeNull()
    expect(safeExternalHref('../x')).toBeNull()
  })
})
