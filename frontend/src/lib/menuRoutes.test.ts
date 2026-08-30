import { readFileSync } from 'node:fs'
import { fileURLToPath } from 'node:url'

import { describe, expect, it } from 'vitest'

// 菜单上的每一条路径，路由表里都必须有对应的一条。
//
// 这条测试是为一个具体的坏法写的：**路由表没有兜底路由**。地址匹配不上时
// matched 是空的，连外面那层 Shell 都不渲染——用户看到的是一整块白，没有
// 菜单、没有返回入口，也没有任何报错。菜单上摆着一个指向不存在地址的入口，
// 就是一个点了会白屏的按钮。
//
// 改名的时候尤其容易：改了 router.ts 忘了改 Shell.vue，或者反过来。
// 网关那边有同款的 routes_test.go，这是它在前端的对应物。

const src = (rel: string) =>
  readFileSync(fileURLToPath(new URL(`../${rel}`, import.meta.url)), 'utf8')

// Shell.vue 里的菜单项写成 { path: '/xxx', label: ... }。
function menuPaths(): string[] {
  const shell = src('pages/Shell.vue')
  return [...shell.matchAll(/\{\s*path:\s*'(\/[^']*)'/g)].map((m) => m[1])
}

// router.ts 里的子路由写成相对路径（'supplier-recon'），redirect 的那些
// 也算数——它们同样是能落地的地址。
function routePaths(): Set<string> {
  const router = src('router.ts')
  const out = new Set<string>()
  for (const m of router.matchAll(/path:\s*'([^']*)'/g)) {
    const p = m[1]
    out.add(p.startsWith('/') ? p : `/${p}`)
  }
  return out
}

describe('菜单和路由表', () => {
  it('菜单上的每条路径在路由表里都有——否则点进去是一整块白', () => {
    const routes = routePaths()
    // 带参数的子路径（/foo/:id）不会出现在菜单上，但父路径存在就算落得了地。
    const reachable = (p: string) =>
      routes.has(p) || [...routes].some((r) => r !== '/' && p.startsWith(`${r}/`))
    const dangling = menuPaths().filter((p) => !reachable(p))
    expect(dangling, `这些菜单项指向不存在的地址：\n${dangling.join('\n')}`).toEqual([])
  })

  // 菜单名和**页面大标题**都要比。只比菜单名的话，改了菜单忘了改页面标题，
  // 两个页面的 H1 一字不差：工单、截图、口头沟通里说「供应商对账页」谁也
  // 分不清指哪一个。三种语言各比一遍。
  it('对账页和往来页的名字不能撞——菜单名和页面标题都算', () => {
    for (const lang of ['zh', 'en', 'es']) {
      const text = src(`locales/${lang}.ts`)
      const navRecon = /financeNav:[\s\S]*?supplierRecon:\s*'([^']*)'/.exec(text)?.[1]
      const navLedger = /financeNav:[\s\S]*?supplierStatements:\s*'([^']*)'/.exec(text)?.[1]
      const titleRecon = /supplierRecon:\s*\{[\s\S]*?title:\s*'([^']*)'/.exec(text)?.[1]
      const titleLedger = /supplierStatements:\s*\{[\s\S]*?title:\s*'([^']*)'/.exec(text)?.[1]
      for (const v of [navRecon, navLedger, titleRecon, titleLedger]) expect(v, lang).toBeTruthy()
      expect(navRecon, `${lang} 菜单名撞了`).not.toBe(navLedger)
      expect(titleRecon, `${lang} 页面标题撞了`).not.toBe(titleLedger)
    }
  })
})
