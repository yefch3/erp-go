import { readFileSync, readdirSync, statSync } from 'node:fs'
import { join } from 'node:path'
import { describe, expect, it } from 'vitest'

/**
 * 请求路径不能自带 /api 前缀。
 *
 * api.ts 里 `axios.create({ baseURL: '/api' })` 已经补了这一段，所以调用处再写
 * 一遍就会打到 /api/api/…。chi 匹配不上，返回的是**纯文本**的 404，不是带
 * message 的错误信封——页面上弹出来的是 axios 的英文原话
 * 「Request failed with status code 404」，看不出是路径写错了。
 *
 * 这是线上真的发生过的：附件预览那条路径写成了 `/api/inbound-mails/...`，
 * 按钮点下去必然失败，而后端的路由、权限、转换全都是好的，从错误消息上
 * 一点看不出问题在前端。
 *
 * 一条 grep 式的测试，比在每个调用处小心翼翼靠谱。
 */
function sourceFiles(dir: string): string[] {
  const out: string[] = []
  for (const name of readdirSync(dir)) {
    const p = join(dir, name)
    if (statSync(p).isDirectory()) {
      out.push(...sourceFiles(p))
    } else if (/\.(ts|vue)$/.test(name) && !/\.test\.ts$/.test(name)) {
      out.push(p)
    }
  }
  return out
}

describe('请求路径', () => {
  it('调用处不自带 /api 前缀（baseURL 已经有了）', () => {
    // get('/api/x')、post(`/api/x`)、del('/api/x') 之类。api.ts 自己不算：
    // 前缀就是在那里定义的。
    //
    // 整个文件一起匹配，不是逐行：真正出问题的那一次就是跨行写的——
    // `post<{ previewUrl?: string }>(` 在一行，路径在下一行，逐行匹配看不见。
    const call = /\b(?:get|post|put|patch|del|download|postDownload)\s*(?:<[^()]*>)?\s*\(\s*[`'"]\/api\//g
    const bad: string[] = []
    for (const file of sourceFiles(join(__dirname, '..'))) {
      if (file.endsWith('/api.ts')) continue
      const src = readFileSync(file, 'utf8')
      for (const m of src.matchAll(call)) {
        const line = src.slice(0, m.index).split('\n').length
        bad.push(`${file.replace(/.*\/src\//, 'src/')}:${line}  ${m[0].replace(/\s+/g, ' ')}…`)
      }
    }
    expect(bad, `这些路径会打到 /api/api/…：\n${bad.join('\n')}`).toEqual([])
  })
})
