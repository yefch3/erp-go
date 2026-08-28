import { describe, expect, it } from 'vitest'
import {
  buildDepartmentTree,
  buildReportingTree,
  countMembers,
  focusView,
  nodeMatches,
  type OrgDepartment,
  type OrgMember,
} from './orgChart'

function member(id: string, name: string, managerId = '0', departmentId = '1'): OrgMember {
  return {
    id,
    code: 'E' + id,
    name,
    englishName: '',
    position: '',
    status: 'ACTIVE',
    managerId,
    departmentId,
    departmentName: '部门' + departmentId,
    avatarUrl: '',
    leaveDate: '',
  }
}

function dept(id: string, name: string, parentId = '0', sortOrder = 0): OrgDepartment {
  return { id, name, parentId, sortOrder, status: 'ACTIVE', leaderEmployeeId: '0' }
}

// 这一组钉的全是「人被画丢了」——那是这张图最坏的失败方式：
// 少一个方块，谁都不会发现，除非那个人自己去找自己。
describe('汇报线', () => {
  it('头上没人的都在顶层——这是一片森林，不是一棵树', () => {
    const tree = buildReportingTree([
      member('1', '老板'),
      member('2', '独立顾问'),
      member('3', '下属', '1'),
    ])
    expect(tree.map((n) => n.label)).toEqual(['独立顾问', '老板'])
    expect(tree.find((n) => n.label === '老板')!.children.map((c) => c.label)).toEqual(['下属'])
  })

  it('上级不在名单里的人**不能丢**，提到顶层并标记', () => {
    // 名单只含在职员工，所以上级一离职，下属的 managerId 就指向了空气。
    const tree = buildReportingTree([member('1', '老板'), member('5', '孤儿', '999')])
    const orphan = tree.find((n) => n.label === '孤儿')
    expect(orphan, '上级离职后，下属被整个丢掉了').toBeDefined()
    expect(orphan!.orphaned).toBe(true)
    expect(countMembers(tree)).toBe(2)
  })

  it('成环不会转不停，人也不会丢', () => {
    // 写入时后端拦了环，但读的这一侧不能假设数据一定干净——
    // 真环了，朴素写法是死循环，浏览器直接卡死。
    const tree = buildReportingTree([
      member('1', '甲', '2'),
      member('2', '乙', '3'),
      member('3', '丙', '1'),
    ])
    expect(countMembers(tree)).toBe(3)
  })

  it('把自己填成自己的上级也不会丢', () => {
    const tree = buildReportingTree([member('1', '自环', '1')])
    expect(tree.map((n) => n.label)).toEqual(['自环'])
    expect(countMembers(tree)).toBe(1)
  })

  it('每个人都恰好出现一次', () => {
    const people = [
      member('1', '一'),
      member('2', '二', '1'),
      member('3', '三', '1'),
      member('4', '四', '2'),
      member('5', '五', '404'), // 上级不在
    ]
    expect(countMembers(buildReportingTree(people))).toBe(people.length)
  })

  it('顺序是稳定的——每次刷新都变的话，看的人会以为组织变了', () => {
    const people = [member('3', '丙'), member('1', '甲'), member('2', '乙')]
    const once = buildReportingTree(people).map((n) => n.label)
    const twice = buildReportingTree([...people].reverse()).map((n) => n.label)
    expect(once).toEqual(twice)
  })
})

describe('部门树', () => {
  it('空部门照样显示——刚成立还是被掏空了，正是想知道的事', () => {
    const tree = buildDepartmentTree([dept('1', '销售部'), dept('2', '新部门')], [])
    expect(tree.map((n) => n.label)).toEqual(['销售部', '新部门'])
  })

  it('部门嵌套按 parentId，人挂在自己的部门下', () => {
    const tree = buildDepartmentTree(
      [dept('1', '总部'), dept('2', '华东', '1')],
      [member('10', '张三', '0', '2')],
    )
    expect(tree).toHaveLength(1)
    const east = tree[0].children.find((c) => c.label === '华东')!
    expect(east.children.map((c) => c.label)).toEqual(['张三'])
  })

  it('父部门不存在的子部门提到顶层，不丢', () => {
    const tree = buildDepartmentTree([dept('2', '孤儿部门', '999')], [])
    expect(tree.map((n) => n.label)).toEqual(['孤儿部门'])
  })

  it('部门不在部门表里的人归到「未分配」，不能凭空消失', () => {
    const tree = buildDepartmentTree([dept('1', '销售部')], [member('10', '流浪者', '0', '404')])
    const bucket = tree.find((n) => n.label === '未分配部门')
    expect(bucket, '部门数据出问题时，受影响的人必须看得见').toBeDefined()
    expect(bucket!.children.map((c) => c.label)).toEqual(['流浪者'])
    expect(countMembers(tree)).toBe(1)
  })

  it('停用的部门不画，但它底下的人要归到「未分配」而不是消失', () => {
    const gone = dept('2', '已撤销', '0')
    gone.status = 'INACTIVE'
    const tree = buildDepartmentTree([dept('1', '销售部'), gone], [member('10', '还在的人', '0', '2')])
    expect(tree.some((n) => n.label === '已撤销')).toBe(false)
    expect(countMembers(tree)).toBe(1)
  })

  it('部门排在人前面', () => {
    const tree = buildDepartmentTree(
      [dept('1', '总部'), dept('2', '子部门', '1')],
      [member('10', '阿甲', '0', '1')],
    )
    expect(tree[0].children.map((c) => c.kind)).toEqual(['department', 'member'])
  })
})

describe('搜索', () => {
  const node = {
    key: 'm1',
    kind: 'member' as const,
    label: '张三',
    member: member('1', '张三'),
    children: [],
  }
  node.member.position = '销售经理'
  node.member.englishName = 'Sam'

  it('空关键词全留', () => {
    expect(nodeMatches(node, '')).toBe(true)
    expect(nodeMatches(node, '   ')).toBe(true)
  })

  it('姓名、工号、英文名、岗位、部门都能搜到', () => {
    for (const q of ['张三', 'E1', 'sam', '销售', '部门1']) {
      expect(nodeMatches(node, q), `搜「${q}」应该匹配`).toBe(true)
    }
  })

  it('搜不着就是搜不着', () => {
    expect(nodeMatches(node, '李四')).toBe(false)
  })
})

// 「以自己为中心」的那张图。全公司三百个方块铺满屏幕等于什么都没说，
// 每次只回答「这个人在组织里的位置」——所以这一组钉的是「一圈人取对了没有」。
describe('以某人为中心', () => {
  //   甲（无上级）
  //   ├─ 乙  ← 中心
  //   │   ├─ 丁
  //   │   └─ 戊
  //   └─ 丙
  const people = [
    member('1', '甲'),
    member('2', '乙', '1'),
    member('3', '丙', '1'),
    member('4', '丁', '2'),
    member('5', '戊', '2'),
  ]

  it('上级链从最高排到直属上级', () => {
    const v = focusView(people, '4')
    expect(v.ancestors.map((a) => a.name)).toEqual(['甲', '乙'])
  })

  it('同级里**包含自己**——抽掉自己，中心那个方块就无处安放了', () => {
    const v = focusView(people, '2')
    expect(v.peers.map((p) => p.name)).toEqual(['丙', '乙'])
    expect(v.peers.some((p) => p.id === '2')).toBe(true)
  })

  it('下属只算直接下属，不含孙子辈', () => {
    expect(focusView(people, '1').reports.map((r) => r.name)).toEqual(['丙', '乙'])
    expect(focusView(people, '2').reports.map((r) => r.name)).toEqual(['丁', '戊'])
  })

  it('头上没人时，同级是所有头上没人的人', () => {
    const tops = [member('1', '甲'), member('2', '乙'), member('3', '丙', '1')]
    const v = focusView(tops, '1')
    expect(v.ancestors).toEqual([])
    // 按拼音：甲(jiǎ) 在 乙(yǐ) 前面
    expect(v.peers.map((p) => p.name)).toEqual(['甲', '乙'])
  })

  it('上级已离职（不在名单里）——链子到此为止，不是崩掉', () => {
    const v = focusView([member('9', '孤儿', '404')], '9')
    expect(v.ancestors).toEqual([])
    expect(v.focus?.name).toBe('孤儿')
    // 上级查不到，就和「头上没人」同列——否则这个人会连同级都没有。
    expect(v.peers.map((p) => p.name)).toEqual(['孤儿'])
  })

  it('成环不会转不停', () => {
    const ring = [member('1', '甲', '2'), member('2', '乙', '3'), member('3', '丙', '1')]
    const v = focusView(ring, '1')
    expect(v.ancestors.length).toBeLessThanOrEqual(3)
    expect(v.focus?.name).toBe('甲')
  })

  it('中心不在名单里就是空视图，页面据此说「找不到」', () => {
    const v = focusView(people, '999')
    expect(v.focus).toBeUndefined()
    expect(v.peers).toEqual([])
    expect(v.reports).toEqual([])
  })
})
