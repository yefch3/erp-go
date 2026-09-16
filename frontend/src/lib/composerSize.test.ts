import { describe, expect, it } from 'vitest'
import {
  COMPOSER_DEFAULT,
  COMPOSER_MIN,
  clampComposerSize,
  readComposerSize,
  resizedSize,
  writeComposerSize,
} from './composerSize'

const viewport = { w: 1440, h: 900 }

describe('写信窗拖动改大小', () => {
  it('窗子贴右下角：往左拖变宽，往上拖变高', () => {
    const start = { w: 620, h: 640 }
    expect(resizedSize(start, 'nw', -100, -50, viewport)).toEqual({ w: 720, h: 690 })
  })

  it('拖左边只改宽，拖上边只改高', () => {
    const start = { w: 620, h: 640 }
    expect(resizedSize(start, 'w', -100, -50, viewport)).toEqual({ w: 720, h: 640 })
    expect(resizedSize(start, 'n', -100, -50, viewport)).toEqual({ w: 620, h: 690 })
  })

  it('往右往下拖是缩小，缩到最小值停', () => {
    const start = { w: 620, h: 640 }
    expect(resizedSize(start, 'nw', 900, 900, viewport)).toEqual(COMPOSER_MIN)
  })

  it('最大不超过屏幕减去边距', () => {
    expect(clampComposerSize({ w: 5000, h: 5000 }, viewport)).toEqual({ w: 1440 - 48, h: 900 - 48 })
  })

  it('屏幕比最小值还小时以最小值为准，不会算出负数', () => {
    expect(clampComposerSize({ w: 100, h: 100 }, { w: 300, h: 200 })).toEqual(COMPOSER_MIN)
  })

  it('四舍五入成整数像素', () => {
    expect(clampComposerSize({ w: 620.4, h: 640.6 }, viewport)).toEqual({ w: 620, h: 641 })
  })
})

describe('记住大小', () => {
  function memory(initial: Record<string, string> = {}) {
    const m = { ...initial }
    return {
      getItem: (k: string) => m[k] ?? null,
      setItem: (k: string, v: string) => {
        m[k] = v
      },
      dump: () => m,
    }
  }

  it('记了就读得回来', () => {
    const s = memory()
    writeComposerSize(s, { w: 800, h: 700 })
    expect(readComposerSize(s)).toEqual({ w: 800, h: 700 })
  })

  it('没记过、记坏了、没有 storage：都回默认', () => {
    expect(readComposerSize(memory())).toEqual(COMPOSER_DEFAULT)
    expect(readComposerSize(memory({ 'mail.composerSize': '{oops' }))).toEqual(COMPOSER_DEFAULT)
    expect(readComposerSize(memory({ 'mail.composerSize': '{"w":"wide"}' }))).toEqual(COMPOSER_DEFAULT)
    expect(readComposerSize(null)).toEqual(COMPOSER_DEFAULT)
  })

  it('storage 抛异常（无痕模式）不炸', () => {
    const broken = {
      getItem: () => {
        throw new Error('denied')
      },
      setItem: () => {
        throw new Error('denied')
      },
    }
    expect(readComposerSize(broken)).toEqual(COMPOSER_DEFAULT)
    expect(() => writeComposerSize(broken, COMPOSER_DEFAULT)).not.toThrow()
  })
})
