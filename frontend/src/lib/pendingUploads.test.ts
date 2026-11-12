import { describe, expect, it } from 'vitest'

import {
  MAX_UPLOAD_BYTES,
  addPicked,
  categoryForNext,
  dropRegistered,
  formatFileSize,
  patchPending,
  removePending,
  sameFile,
  uploadPending,
  type PendingUpload,
  type UploadSteps,
} from './pendingUploads'

// 清单只读 name / size / lastModified，用最小替身：单测跑在 node 里，没有 File。
const aFile = (name: string, size = 1200, lastModified = 1) => ({ name, size, lastModified }) as unknown as File

function ids() {
  let n = 0
  return () => `u${++n}`
}

describe('addPicked', () => {
  it('往后加，不替换已经选好的——原来的选择框每选一次就把上一次换掉', () => {
    const next = ids()
    const first = addPicked([], [aFile('发票1.pdf')], 'INVOICE', next)
    const second = addPicked(first.list, [aFile('行程单.pdf'), aFile('收据.jpg')], 'ITINERARY', next)

    expect(second.added).toBe(2)
    expect(second.list.map((item) => [item.file.name, item.category])).toEqual([
      ['发票1.pdf', 'INVOICE'],
      ['行程单.pdf', 'ITINERARY'],
      ['收据.jpg', 'ITINERARY'],
    ])
    expect(second.list.every((item) => item.status === 'waiting' && item.error === '')).toBe(true)
    // id 各不相同：同名文件可以有两份，v-for 不能拿文件名当 key。
    expect(new Set(second.list.map((item) => item.id)).size).toBe(3)
  })

  it('不就地改旧清单', () => {
    const first = addPicked([], [aFile('a.pdf')], 'INVOICE', ids())
    const snapshot = [...first.list]
    addPicked(first.list, [aFile('b.pdf')], 'INVOICE', ids())
    expect(first.list).toEqual(snapshot)
  })

  it('同一份文件选了两遍只留一份，并说出是哪份', () => {
    const next = ids()
    const first = addPicked([], [aFile('发票.pdf', 5000, 77)], 'INVOICE', next)
    const again = addPicked(first.list, [aFile('发票.pdf', 5000, 77), aFile('发票.pdf', 5001, 77)], 'INVOICE', next)

    expect(again.duplicates).toEqual(['发票.pdf'])
    // 同名但大小不同是另一份文件，照收。
    expect(again.added).toBe(1)
    expect(again.list).toHaveLength(2)
  })

  it('同一次选择里的重复也只留一份', () => {
    const out = addPicked([], [aFile('a.pdf'), aFile('a.pdf')], 'INVOICE', ids())
    expect(out.added).toBe(1)
    expect(out.duplicates).toEqual(['a.pdf'])
  })

  it('空文件和超过上限的不收，各自记下，别的照收', () => {
    const out = addPicked(
      [],
      [aFile('空的.pdf', 0), aFile('太大.mp4', MAX_UPLOAD_BYTES + 1), aFile('刚好.zip', MAX_UPLOAD_BYTES), aFile('正常.pdf')],
      'OTHER',
      ids(),
    )
    expect(out.empty).toEqual(['空的.pdf'])
    expect(out.tooLarge).toEqual(['太大.mp4'])
    expect(out.list.map((item) => item.file.name)).toEqual(['刚好.zip', '正常.pdf'])
    expect(out.added).toBe(2)
  })

  it('上限和网关中转、前门 nginx 是同一个数', () => {
    expect(MAX_UPLOAD_BYTES).toBe(64 * 1024 * 1024)
  })
})

describe('removePending / patchPending', () => {
  const list = (): PendingUpload[] => addPicked([], [aFile('a.pdf'), aFile('b.pdf')], 'INVOICE', ids()).list

  it('按 id 拿掉一份', () => {
    const before = list()
    const after = removePending(before, 'u1')
    expect(after.map((item) => item.file.name)).toEqual(['b.pdf'])
    expect(before).toHaveLength(2)
  })

  it('只改指定那一份的指定字段', () => {
    const before = list()
    const after = patchPending(before, 'u2', { status: 'failed', error: '文件存储拒绝了这次上传' })
    expect(after[0]).toBe(before[0])
    expect(after[1]).toMatchObject({ id: 'u2', status: 'failed', error: '文件存储拒绝了这次上传', category: 'INVOICE' })
    expect(before[1].status).toBe('waiting')
  })

  it('找不到 id 时原样还回去', () => {
    const before = list()
    expect(patchPending(before, 'nope', { category: 'OTHER' })).toEqual(before)
    expect(removePending(before, 'nope')).toEqual(before)
  })
})

describe('categoryForNext', () => {
  it('跟着清单里最后一份走，清单空着用默认值', () => {
    expect(categoryForNext([], 'INVOICE')).toBe('INVOICE')
    const one = addPicked([], [aFile('a.pdf')], 'INVOICE', ids()).list
    const changed = patchPending(one, 'u1', { category: 'RECEIPT' })
    expect(categoryForNext(changed, 'INVOICE')).toBe('RECEIPT')
  })
})

describe('sameFile', () => {
  it('名字、大小、修改时间都一样才算同一份', () => {
    expect(sameFile(aFile('a.pdf', 10, 1), aFile('a.pdf', 10, 1))).toBe(true)
    expect(sameFile(aFile('a.pdf', 10, 1), aFile('a.pdf', 10, 2))).toBe(false)
    expect(sameFile(aFile('a.pdf', 10, 1), aFile('A.pdf', 10, 1))).toBe(false)
  })
})

describe('formatFileSize', () => {
  it('小的按 KB、大的按 MB，不出现 0 KB', () => {
    expect(formatFileSize(1)).toBe('1 KB')
    expect(formatFileSize(1024)).toBe('1 KB')
    expect(formatFileSize(1025)).toBe('2 KB')
    expect(formatFileSize(1024 * 1024)).toBe('1.0 MB')
    expect(formatFileSize(5.5 * 1024 * 1024)).toBe('5.5 MB')
  })
})

// uploadPending 的三步都换成替身：记下被调用的顺序，按吩咐成功或失败。
function fakeSteps(overrides: Partial<UploadSteps> = {}) {
  const calls: string[] = []
  const steps: UploadSteps = {
    registeredKeys: new Set<string>(),
    presign: async (item) => {
      calls.push(`presign ${item.file.name}`)
      return { key: `key/${item.file.name}`, uploadUrl: `https://store/${item.file.name}` }
    },
    send: async (uploadUrl) => {
      calls.push(`send ${uploadUrl.replace('https://store/', '')}`)
    },
    register: async (item, key) => {
      calls.push(`register ${key} as ${item.category}`)
    },
    reason: (err) => (err instanceof Error ? err.message : String(err)),
    timedOut: (err) => err instanceof Error && err.message === 'timeout',
    ...overrides,
  }
  return { calls, steps }
}

const threeFiles = (): PendingUpload[] =>
  addPicked([], [aFile('a.pdf'), aFile('b.pdf'), aFile('c.pdf')], 'INVOICE', ids()).list

describe('uploadPending', () => {
  it('一份一份走完三步，交上去的从清单里拿掉；清单空了就是全交上去了', async () => {
    const { calls, steps } = fakeSteps()
    const seen: PendingUpload[][] = []
    const left = await uploadPending(threeFiles(), steps, (list) => seen.push(list))

    expect(left).toEqual([])
    expect(calls).toEqual([
      'presign a.pdf', 'send a.pdf', 'register key/a.pdf as INVOICE',
      'presign b.pdf', 'send b.pdf', 'register key/b.pdf as INVOICE',
      'presign c.pdf', 'send c.pdf', 'register key/c.pdf as INVOICE',
    ])
    // 页面每一步都拿到一张新清单：先是「a 在传」，最后一张是空的。
    expect(seen[0].map((item) => item.status)).toEqual(['uploading', 'waiting', 'waiting'])
    expect(seen[seen.length - 1]).toEqual([])
  })

  it('不就地改交进来的清单', async () => {
    const list = threeFiles()
    const snapshot = list.map((item) => ({ ...item }))
    await uploadPending(list, fakeSteps().steps, () => {})
    expect(list).toEqual(snapshot)
  })

  it('送文件失败只记在那一份上，后面的照常交——一张发票传不上去不该拦住其余几张', async () => {
    const { calls, steps } = fakeSteps({
      send: async (uploadUrl) => {
        if (uploadUrl.endsWith('b.pdf')) throw new Error('文件存储拒绝了这次上传')
      },
    })
    const left = await uploadPending(threeFiles(), steps, () => {})

    expect(left).toHaveLength(1)
    expect(left[0]).toMatchObject({ status: 'failed', error: '文件存储拒绝了这次上传', uploadedKey: '' })
    expect(left[0].file.name).toBe('b.pdf')
    expect(calls).toContain('register key/c.pdf as INVOICE')
    expect(calls).not.toContain('register key/b.pdf as INVOICE')
  })

  it('等到超时就整批停下，后面的留着没动', async () => {
    const { calls, steps } = fakeSteps({
      send: async () => {
        throw new Error('timeout')
      },
    })
    const left = await uploadPending(threeFiles(), steps, () => {})

    expect(left.map((item) => item.status)).toEqual(['failed', 'waiting', 'waiting'])
    expect(calls).toEqual(['presign a.pdf'])
  })

  it('要地址失败是单子的事，整批停下——不拿同一句报错刷满清单', async () => {
    const { calls, steps } = fakeSteps({
      presign: async (item) => {
        calls.push(`presign ${item.file.name}`)
        throw new Error('当前审批或付款状态不能修改报销凭证')
      },
    })
    const left = await uploadPending(threeFiles(), steps, () => {})

    expect(calls).toEqual(['presign a.pdf'])
    expect(left.map((item) => item.status)).toEqual(['failed', 'waiting', 'waiting'])
    expect(left[0].error).toBe('当前审批或付款状态不能修改报销凭证')
  })

  it('登记失败也整批停下，但记住文件已经在存储里', async () => {
    const { calls, steps } = fakeSteps({
      register: async (_item, key) => {
        calls.push(`register ${key}`)
        throw new Error('网络错误')
      },
    })
    const left = await uploadPending(threeFiles(), steps, () => {})

    expect(calls).toEqual(['presign a.pdf', 'send a.pdf', 'register key/a.pdf'])
    expect(left[0]).toMatchObject({ status: 'failed', uploadedKey: 'key/a.pdf' })
    expect(left.slice(1).map((item) => item.status)).toEqual(['waiting', 'waiting'])
  })

  it('再来一次时，已经在存储里的不重新传，只补登记', async () => {
    // 第一次：a 传上去了，登记失败。
    const first = fakeSteps({
      register: async () => {
        throw new Error('网络错误')
      },
    })
    const afterFirst = await uploadPending(threeFiles(), first.steps, () => {})

    // 第二次：一切正常。
    const second = fakeSteps()
    const afterSecond = await uploadPending(afterFirst, second.steps, () => {})

    expect(afterSecond).toEqual([])
    expect(second.calls.slice(0, 2)).toEqual(['register key/a.pdf as INVOICE', 'presign b.pdf'])
    expect(second.calls).not.toContain('presign a.pdf')
    expect(second.calls).not.toContain('send a.pdf')
  })

  it('上次登记其实成功了（只是没等到回音）：不再登记第二遍，直接拿掉', async () => {
    const first = fakeSteps({
      register: async () => {
        throw new Error('timeout')
      },
    })
    const afterFirst = await uploadPending(threeFiles().slice(0, 1), first.steps, () => {})
    expect(afterFirst[0]).toMatchObject({ status: 'failed', uploadedKey: 'key/a.pdf' })

    // 这次保存时，服务端的单子上已经有这个键了。
    const second = fakeSteps({ registeredKeys: new Set(['key/a.pdf']) })
    const afterSecond = await uploadPending(afterFirst, second.steps, () => {})

    expect(afterSecond).toEqual([])
    expect(second.calls).toEqual([])
  })

  it('补登记时用的是清单里现在的类型', async () => {
    const first = fakeSteps({
      register: async () => {
        throw new Error('网络错误')
      },
    })
    const afterFirst = await uploadPending(threeFiles().slice(0, 1), first.steps, () => {})
    const recategorised = patchPending(afterFirst, afterFirst[0].id, { category: 'RECEIPT' })

    const second = fakeSteps()
    await uploadPending(recategorised, second.steps, () => {})
    expect(second.calls).toEqual(['register key/a.pdf as RECEIPT'])
  })

  it('重试时先把上一次的失败原因清掉', async () => {
    const failing = fakeSteps({
      send: async () => {
        throw new Error('第一次的原因')
      },
    })
    const afterFirst = await uploadPending(threeFiles().slice(0, 1), failing.steps, () => {})
    expect(afterFirst[0].error).toBe('第一次的原因')

    const seen: PendingUpload[][] = []
    await uploadPending(afterFirst, fakeSteps().steps, (list) => seen.push(list))
    expect(seen[0][0]).toMatchObject({ status: 'uploading', error: '' })
  })
})

describe('dropRegistered', () => {
  it('服务端已经有的键对应的那几份拿掉，别的不动', () => {
    const list = patchPending(
      patchPending(threeFiles(), 'u1', { status: 'failed', error: '没等到回音', uploadedKey: 'key/a.pdf' }),
      'u2',
      { status: 'failed', error: '登记失败', uploadedKey: 'key/b.pdf' },
    )
    const left = dropRegistered(list, new Set(['key/a.pdf', 'key/别人的.pdf']))
    // a 其实登记上了 → 拿掉；b 进了存储但没登记上 → 留着等补登记；c 还没传 → 留着。
    expect(left.map((item) => item.file.name)).toEqual(['b.pdf', 'c.pdf'])
  })

  it('还没进存储的不会因为空键被误删', () => {
    expect(dropRegistered(threeFiles(), new Set(['']))).toHaveLength(3)
  })
})
