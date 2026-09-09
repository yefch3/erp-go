import { readFileSync, readdirSync, statSync } from 'node:fs'
import { join } from 'node:path'
import { describe, expect, test } from 'vitest'

// .vue 文件里，最后一个块之后不许再有东西。
//
// 这条测试是被一次真事逼出来的：往 EmailsPage.vue 追加了 60 行三栏样式，
// 结果追加到了 </style> **后面**。SFC 解析器把块外的内容当垃圾直接丢掉，
// 于是那 60 行 CSS 一条都没生效——页面上阅读区堆在列表上面，而不是左右并排。
//
// 最要命的是**两道门禁都拦不住**：vue-tsc 过了（那不是 TypeScript），
// npm run build 也过了（尾巴不影响编译，产物里就是没有那段样式）。绿灯
// 一路亮到线上，我还照着「build 通过」说结构是合法的。
//
// 所以钉在这里：一个字符都不许漏在块外。
const SRC = new URL('..', import.meta.url).pathname

function vueFiles(dir: string): string[] {
  return readdirSync(dir).flatMap((name) => {
    const full = join(dir, name)
    if (statSync(full).isDirectory()) return vueFiles(full)
    return name.endsWith('.vue') ? [full] : []
  })
}

describe('单文件组件的结构', () => {
  test('最后一个块之后不许有内容——块外的东西会被静默丢掉', () => {
    const stray: string[] = []
    for (const file of vueFiles(SRC)) {
      const text = readFileSync(file, 'utf8')
      // 最后一个顶层结束标签之后还剩什么。三种块都要看：漏在哪一种后面
      // 都是同一个后果。
      const last = Math.max(
        text.lastIndexOf('</template>'),
        text.lastIndexOf('</script>'),
        text.lastIndexOf('</style>'),
      )
      if (last < 0) continue
      const tail = text.slice(last).replace(/^<\/(template|script|style)>/, '')
      if (tail.trim() !== '') {
        stray.push(`${file.replace(SRC, '')}：块外还剩 ${tail.trim().length} 个字符`)
      }
    }
    expect(stray, `这些文件在最后一个块之后还有内容，那部分会被静默丢掉：\n${stray.join('\n')}`)
      .toEqual([])
  })
})
