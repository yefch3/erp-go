import { readFileSync } from 'node:fs'
import { fileURLToPath } from 'node:url'
import { describe, expect, test } from 'vitest'

const page = readFileSync(
  fileURLToPath(new URL('../pages/EmailsPage.vue', import.meta.url)),
  'utf8',
)

function cssRule(selector: string): string {
  const escaped = selector.replace(/[.*+?^${}()|[\]\\]/g, '\\$&')
  const match = page.match(new RegExp(`${escaped}\\s*\\{([^}]*)\\}`))
  expect(match, `EmailsPage.vue 应包含 ${selector} 样式块`).not.toBeNull()
  return match?.[1] ?? ''
}

describe('邮箱三栏布局', () => {
  test('文件夹、邮件列表和阅读区底边齐平，各自内部滚动', () => {
    for (const selector of ['.rail', '.list-col', '.reader-col']) {
      const rule = cssRule(selector)
      expect(rule).toMatch(/height:\s*100%/)
      expect(rule).toMatch(/overflow-y:\s*auto/)
      expect(rule).toMatch(/box-sizing:\s*border-box/)
    }

    expect(cssRule('.panes')).toMatch(/min-height:\s*0/)
    expect(cssRule('.panes')).toMatch(/align-items:\s*stretch/)
    expect(cssRule('.pane')).toMatch(/padding:\s*14px\s+16px\s+0/)
  })
})
