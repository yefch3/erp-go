import { readFileSync, readdirSync, statSync } from 'node:fs'
import { join } from 'node:path'
import { describe, expect, it } from 'vitest'

import zhMessages from '../locales/zh'
import enMessages from '../locales/en'
import esMessages from '../locales/es'

// 界面上出现一个 `platform.createConfirm` 这样的字面量，是最不体面的那种错。
//
// 它真的发生过：开户弹窗的主按钮（会真的建公司、发邀请信的那颗）上写着
// 「platform.createConfirm」。三种语言全都没有这个词条，vue-i18n 找不到就把
// 键名原样吐出来。
//
// 为什么原来的检查没抓到：比对的是 zh / en / es 三份**互相**对齐——而一个
// 三份都没有的词条，三份是完全一致的。对齐检查天然看不见它。所以这里换个
// 方向：拿**页面里真正用到的键**去对 locale。
//
// 词条表直接 import 进来展平，不去正则解析源文件——单行写的嵌套对象
// （`statuses: { OPEN: '待处理' }`）用正则数括号必然数错，第一版就在这上面
// 报了一串不存在的缺失。

const SRC = join(__dirname, '..')

function flatten(obj: unknown, prefix = '', out = new Set<string>()): Set<string> {
  if (!obj || typeof obj !== 'object') return out
  for (const [k, v] of Object.entries(obj as Record<string, unknown>)) {
    const path = prefix ? `${prefix}.${k}` : k
    out.add(path)
    if (v && typeof v === 'object') flatten(v, path, out)
  }
  return out
}

function sourceFiles(dir: string, out: string[] = []): string[] {
  for (const name of readdirSync(dir)) {
    const path = join(dir, name)
    if (statSync(path).isDirectory()) {
      if (name !== 'locales') sourceFiles(path, out)
      continue
    }
    if (name.endsWith('.vue') || (name.endsWith('.ts') && !name.endsWith('.test.ts'))) {
      out.push(path)
    }
  }
  return out
}

/**
 * 页面和组件里用到的键。
 *
 * 只收写死的字面量。拼出来的键（`t(\`orders.statuses.${row.status}\`)`）这里
 * 判断不了——那种要靠人保证词条表覆盖全部取值，不是这条测试能管的。
 */
function usedKeys(): Map<string, Set<string>> {
  const used = new Map<string, Set<string>>()
  for (const file of sourceFiles(SRC)) {
    const text = readFileSync(file, 'utf8')
    for (const m of text.matchAll(/\bt\(\s*'([a-zA-Z][a-zA-Z0-9_]*(?:\.[a-zA-Z0-9_]+)+)'/g)) {
      const where = used.get(m[1]) ?? new Set<string>()
      where.add(file.slice(SRC.length + 1))
      used.set(m[1], where)
    }
  }
  return used
}

describe('三语言词条', () => {
  const zh = flatten(zhMessages)

  it('页面上用到的每个词条，中文里都得有——否则按钮上会直接显示键名', () => {
    const missing = [...usedKeys()]
      .filter(([key]) => !zh.has(key))
      .map(([key, files]) => `${key}（用在 ${[...files].join(', ')}）`)
    expect(missing, '这些键没有中文文案，界面会原样显示键名：\n' + missing.join('\n')).toEqual([])
  })

  for (const [lang, messages] of [['en', enMessages], ['es', esMessages]] as const) {
    it(`${lang} 和中文一一对应`, () => {
      const other = flatten(messages)
      const missing = [...zh].filter((k) => !other.has(k))
      const extra = [...other].filter((k) => !zh.has(k))
      expect(missing, `${lang} 缺这些：${missing.join(', ')}`).toEqual([])
      expect(extra, `${lang} 多这些：${extra.join(', ')}`).toEqual([])
    })
  }
})
