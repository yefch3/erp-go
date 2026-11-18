// 供应商详情页打开时要备的五样东西：三组下拉选项、「这个人能不能管负责人」、
// 以及「添加负责人」弹窗要用的员工名单。
//
// 它们以前绑在同一个 Promise.all 里，而查员工名单要「查看员工」权限
// （iam:employee:read）。能打开供应商详情的人大多没有这项权限——2026-11 线上
// 只有最高权限和销售经理有，采购、物流、总经理都没有——于是他们一打开详情：
//   · 弹一条红色的「没有权限」——可他们什么越权的事都没做；
//   · Promise.all 整个失败，另外四样跟着没赋值：「编辑基本资料」里业务角色
//     一个勾选框都没有，付款条件的下拉是空的。
// 员工名单只有「添加负责人」用，而那个按钮只给服务端认定「能管负责人」的人。
//
// 所以现在各请求各的，谁失败只少谁；员工名单等服务端答了「能管负责人」才去查。
// 能管负责人、却偏偏没有「查看员工」权限的人仍会看到那条提示，这是该有的：
// 他点得开「添加负责人」，名单却是空的，得有人告诉他为什么。
//
// 放在 lib 而不是 .vue 里，是因为页面组件没有单测。get 由调用方传进来，是因为
// api.ts 一加载就带起 i18n 和 router，node 里跑单测时没有这些。

export interface LookupOption {
  code: string
  label: string
}

/** 员工名单里的一行。接口返回的字段比这多，这里只列用得到的。 */
export interface OwnerCandidate {
  id: string
  code: string
  name: string
  status: string
}

/** 和 api.ts 的 get 同形。 */
export type LookupGet = <T>(url: string, params?: object) => Promise<T>

/**
 * 每一项都是一个**不会失败**的 promise：请求成了给结果，没成给空（列表为空、
 * 权限为否）。各项互不等待，先到的先用。
 */
export interface SupplierDetailLookups {
  /** 业务角色：「编辑基本资料」里的勾选框，页头标签的名字也靠它。 */
  businessOptions: Promise<LookupOption[]>
  /** 付款条件。 */
  paymentOptions: Promise<LookupOption[]>
  /** 负责人的职责。 */
  ownerOptions: Promise<LookupOption[]>
  /** 能不能添加、停用负责人。服务端说了算，页面只据此决定按钮出不出现。 */
  canManageOwners: Promise<boolean>
  /** 「添加负责人」可选的员工；不能管负责人的人这里恒为空，也不会发请求。 */
  employees: Promise<OwnerCandidate[]>
}

const OWNER_CANDIDATE_PAGE_SIZE = 200

/**
 * 请求成了给 pick(结果)，没成给 fallback。
 *
 * 失败不在这里提示：api.ts 的全局拦截器已经为每个失败的请求弹过一次，再报
 * 一遍就是同一件事说两次。这里只保证一项失败不会连累别的项。
 */
async function orFallback<T, R>(request: () => Promise<T>, pick: (data: T) => R, fallback: R): Promise<R> {
  try {
    return pick(await request())
  } catch {
    return fallback
  }
}

function listOf<T>(value: unknown): T[] {
  return Array.isArray(value) ? (value as T[]) : []
}

export function supplierDetailLookups(get: LookupGet): SupplierDetailLookups {
  const optionList = (category: string): Promise<LookupOption[]> =>
    orFallback(
      () => get<{ options?: LookupOption[] }>('/options', { category }),
      (data) => listOf<LookupOption>(data?.options),
      [],
    )

  const canManageOwners = orFallback(
    () => get<{ canManageOwners?: boolean }>('/suppliers/access'),
    (access) => access?.canManageOwners === true,
    false,
  )

  const employees = canManageOwners.then((allowed): OwnerCandidate[] | Promise<OwnerCandidate[]> => {
    if (!allowed) return []
    return orFallback(
      () =>
        get<{ employees?: OwnerCandidate[] }>('/employees', {
          page: 1,
          page_size: OWNER_CANDIDATE_PAGE_SIZE,
          status: 'ACTIVE',
        }),
      (data) => listOf<OwnerCandidate>(data?.employees),
      [],
    )
  })

  return {
    businessOptions: optionList('SUPPLIER_BUSINESS_TYPE'),
    paymentOptions: optionList('PAYMENT_METHOD'),
    ownerOptions: optionList('SUPPLIER_OWNER_RESPONSIBILITY'),
    canManageOwners,
    employees,
  }
}
