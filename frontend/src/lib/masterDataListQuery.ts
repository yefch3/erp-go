import type { LocationQueryValue } from 'vue-router'

export interface MasterDataListFilters {
  keyword?: string
  country?: string
  businessType?: string
  city?: string
  productCategory?: string
  status?: string
  page?: number
}

// 直接用 vue-router 自己的类型，而不是手写一个 string | string[]。
// `?country=` 这种没有值的参数，router 给的是 null 不是空串；写成 string[]
// 的那一版把 (string | null)[] 挡在了门外，于是每一个从 URL 读筛选条件的
// 页面都在这里报错。下面两个函数本来就用 ?? '' 兜住了 null，缺的只是类型。
type QueryValue = LocationQueryValue | LocationQueryValue[] | undefined

/** 从 Vue Router 查询参数中读取单个文本值。 */
export function queryText(value: QueryValue): string {
  if (Array.isArray(value)) return String(value[0] ?? '').trim()
  return String(value ?? '').trim()
}

/** 页码只接受大于零的整数，异常值统一回到第一页。 */
export function queryPage(value: QueryValue): number {
  const page = Number.parseInt(queryText(value), 10)
  return Number.isFinite(page) && page > 0 ? page : 1
}

/**
 * 生成紧凑、可分享的列表查询参数。
 * 空筛选和第一页不写入 URL，避免出现大量没有含义的空参数。
 */
export function masterDataListQuery(filters: MasterDataListFilters): Record<string, string> {
  const query: Record<string, string> = {}
  const values: Array<[string, string | undefined]> = [
    ['keyword', filters.keyword],
    ['country', filters.country],
    ['business_type', filters.businessType],
    ['city', filters.city],
    ['product_category', filters.productCategory],
    ['status', filters.status],
  ]

  for (const [key, value] of values) {
    const normalized = String(value ?? '').trim()
    if (normalized) query[key] = normalized
  }
  if ((filters.page ?? 1) > 1) query.page = String(filters.page)
  return query
}
