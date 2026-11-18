import { describe, expect, it } from 'vitest'

import { supplierDetailLookups, type LookupGet } from './supplierDetailLookups'

// 服务端对「没有这项权限」的回答，和网关真实返回的信封一个样子。
const forbidden = { success: false, code: 'AUTH_PERMISSION_DENIED', message: '没有权限执行此操作' }

type Answer = unknown | (() => unknown)

/**
 * 一个假的 get：按「地址 + 选项分类」查表作答，没登记的地址一律当 404。
 * 同时记下每一次请求，好断言「哪些请求根本没发出去」。
 */
function fakeGet(answers: Record<string, Answer>) {
  const calls: { url: string; params?: Record<string, unknown> }[] = []
  const get = (async (url: string, params?: object) => {
    const p = params as Record<string, unknown> | undefined
    calls.push({ url, params: p })
    const key = p?.category ? `${url}?category=${String(p.category)}` : url
    if (!(key in answers)) throw new Error(`unexpected request: ${key}`)
    const answer = answers[key]
    return typeof answer === 'function' ? (answer as () => unknown)() : answer
  }) as LookupGet
  return { get, calls, urls: () => calls.map((c) => c.url) }
}

const reject = (reason: unknown) => () => Promise.reject(reason)

const business = [{ code: 'GENERAL', label: '一般供应商' }, { code: 'FORWARDER', label: '货代' }]
const payment = [{ code: 'TT', label: '电汇' }]
const responsibility = [{ code: 'PROCUREMENT', label: '采购' }]
const staff = [{ id: '7', code: 'E007', name: '张三', status: 'ACTIVE' }]

function everythingWorks(canManageOwners: boolean): Record<string, Answer> {
  return {
    '/options?category=SUPPLIER_BUSINESS_TYPE': { options: business },
    '/options?category=PAYMENT_METHOD': { options: payment },
    '/options?category=SUPPLIER_OWNER_RESPONSIBILITY': { options: responsibility },
    '/suppliers/access': { canManageOwners, canDelete: canManageOwners },
    '/employees': { employees: staff },
  }
}

async function settle(lookups: ReturnType<typeof supplierDetailLookups>) {
  const [businessOptions, paymentOptions, ownerOptions, canManageOwners, employees] = await Promise.all([
    lookups.businessOptions, lookups.paymentOptions, lookups.ownerOptions, lookups.canManageOwners, lookups.employees,
  ])
  return { businessOptions, paymentOptions, ownerOptions, canManageOwners, employees }
}

describe('supplierDetailLookups', () => {
  // 这就是线上出的那件事：采购、物流、总经理都没有「查看员工」权限，打开
  // 供应商详情却被顺手查了一次员工名单——403，弹一条红色的「没有权限」，
  // 还把同一批里的下拉选项一起带没了。名单只有「添加负责人」用，而那个
  // 按钮他们本来就没有。
  it('不能管负责人的人打开页面，根本不去查员工名单', async () => {
    const { get, urls } = fakeGet({
      ...everythingWorks(false),
      '/employees': reject(forbidden),
    })

    const got = await settle(supplierDetailLookups(get))

    expect(urls()).not.toContain('/employees')
    expect(got.canManageOwners).toBe(false)
    expect(got.employees).toEqual([])
    // 「编辑基本资料」里的业务角色和付款条件照常有得选。
    expect(got.businessOptions).toEqual(business)
    expect(got.paymentOptions).toEqual(payment)
    expect(got.ownerOptions).toEqual(responsibility)
  })

  it('能管负责人的人才去查员工名单，查法和以前一样', async () => {
    const { get, calls } = fakeGet(everythingWorks(true))

    const got = await settle(supplierDetailLookups(get))

    expect(got.canManageOwners).toBe(true)
    expect(got.employees).toEqual(staff)
    expect(calls.filter((c) => c.url === '/employees')).toEqual([
      { url: '/employees', params: { page: 1, page_size: 200, status: 'ACTIVE' } },
    ])
  })

  it('五样东西各请求各的：员工名单查不到，不影响其余四样', async () => {
    const { get } = fakeGet({ ...everythingWorks(true), '/employees': reject(forbidden) })

    const got = await settle(supplierDetailLookups(get))

    expect(got.employees).toEqual([])
    // 按钮照旧给：能不能管是服务端说了算的，名单拿不到是另一回事（拦截器已经提示）。
    expect(got.canManageOwners).toBe(true)
    expect(got.businessOptions).toEqual(business)
    expect(got.paymentOptions).toEqual(payment)
    expect(got.ownerOptions).toEqual(responsibility)
  })

  it('某一组下拉选项拿不到，别的照常', async () => {
    const { get } = fakeGet({
      ...everythingWorks(true),
      '/options?category=PAYMENT_METHOD': reject(new Error('timeout of 15000ms exceeded')),
    })

    const got = await settle(supplierDetailLookups(get))

    expect(got.paymentOptions).toEqual([])
    expect(got.businessOptions).toEqual(business)
    expect(got.ownerOptions).toEqual(responsibility)
    expect(got.canManageOwners).toBe(true)
    expect(got.employees).toEqual(staff)
  })

  // 问不到「能不能管」就当不能：按钮不出现，名单也不去查。服务端本来就会
  // 拦，这里只是不把一个点了必然失败的按钮摆出来。
  it('问不到能不能管负责人时按不能处理，也不去查员工名单', async () => {
    const { get, urls } = fakeGet({ ...everythingWorks(true), '/suppliers/access': reject(new Error('Network Error')) })

    const got = await settle(supplierDetailLookups(get))

    expect(got.canManageOwners).toBe(false)
    expect(got.employees).toEqual([])
    expect(urls()).not.toContain('/employees')
    expect(got.businessOptions).toEqual(business)
  })

  // get 还没来得及返回 promise 就抛了（比如拼请求时出错），也只能少它自己那一项，
  // 不能让整个页面的 onMounted 跟着断掉。
  it('某个请求当场抛错，也只少它自己那一项', async () => {
    const { get } = fakeGet(everythingWorks(true))
    const throwing = ((url: string, params?: object) => {
      if (url === '/suppliers/access') throw new Error('boom')
      return get(url, params)
    }) as LookupGet

    const got = await settle(supplierDetailLookups(throwing))

    expect(got.canManageOwners).toBe(false)
    expect(got.employees).toEqual([])
    expect(got.businessOptions).toEqual(business)
    expect(got.paymentOptions).toEqual(payment)
    expect(got.ownerOptions).toEqual(responsibility)
  })

  it('下拉选项先到先用，不等员工名单', async () => {
    let releaseStaff: (value: unknown) => void = () => {}
    const { get } = fakeGet({
      ...everythingWorks(true),
      '/employees': () => new Promise((resolve) => { releaseStaff = resolve }),
    })

    const lookups = supplierDetailLookups(get)

    // 员工名单还卡着，另外四样已经可以用了。
    await expect(lookups.businessOptions).resolves.toEqual(business)
    await expect(lookups.paymentOptions).resolves.toEqual(payment)
    await expect(lookups.ownerOptions).resolves.toEqual(responsibility)
    await expect(lookups.canManageOwners).resolves.toBe(true)

    releaseStaff({ employees: staff })
    await expect(lookups.employees).resolves.toEqual(staff)
  })

  it('服务端没带列表字段时给空列表，而不是 undefined', async () => {
    const { get } = fakeGet({
      '/options?category=SUPPLIER_BUSINESS_TYPE': {},
      '/options?category=PAYMENT_METHOD': {},
      '/options?category=SUPPLIER_OWNER_RESPONSIBILITY': {},
      '/suppliers/access': { canManageOwners: true },
      '/employees': {},
    })

    const got = await settle(supplierDetailLookups(get))

    expect(got).toEqual({ businessOptions: [], paymentOptions: [], ownerOptions: [], canManageOwners: true, employees: [] })
  })
})
