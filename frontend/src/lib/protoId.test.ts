import { describe, expect, it } from 'vitest'
import { hasId, noId } from './protoId'

describe('hasId', () => {
  // 这一条是整个文件存在的理由。网关开着 EmitUnpopulated，没值的 int64
  // 会变成字符串 "0" 送到浏览器，而 "0" 在 JavaScript 里是真值——
  // 银行流水页就是被这一个字符坑掉了一整列按钮。
  it('把 "0" 当成没有值', () => {
    expect(hasId('0')).toBe(false)
    expect(noId('0')).toBe(true)
  })

  it('空串、null、undefined 都算没有值', () => {
    expect(hasId('')).toBe(false)
    expect(hasId(null)).toBe(false)
    expect(hasId(undefined)).toBe(false)
  })

  it('真的 id 算有值', () => {
    expect(hasId('7')).toBe(true)
    expect(hasId('1234567890123456789')).toBe(true) // 大到 Number 会失精度，照样对
  })

  // 前后空格来自手工拼接的查询串，不该让一个真 id 看起来像空的。
  it('忽略前后空格', () => {
    expect(hasId('  ')).toBe(false)
    expect(hasId(' 7 ')).toBe(true)
  })

  // 数字 0 同样算没有：同一个字段在不同接口里可能是数字也可能是字符串，
  // 调用处不该为此分两种写法。
  it('数字形态也认得', () => {
    expect(hasId(0)).toBe(false)
    expect(hasId(7)).toBe(true)
  })

  // 反过来钉住：不要有人「优化」成 Number(v) !== 0。
  // 超过 2^53 的 id 用 Number 比较会把两个不同的 id 判成相等，
  // 而这里只问「有没有」，字符串比较就够，也不会失精度。
  it('大 id 不经过 Number', () => {
    expect(hasId('9007199254740993')).toBe(true)
    expect(hasId('9007199254740992')).toBe(true)
  })
})
