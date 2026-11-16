// 「待上传」清单：一次要交好几份文件时，页面手里攥着的那张单子。
//
// 起因是出差报销（#591）：一趟差常有好几张发票，而原来的选择框**每选一次就把
// 上一次的换掉**，所有文件还只能共用一个类型——发票和行程单没法一起交。能不能
// 多选其实取决于人知不知道在系统的选文件窗口里按住 Ctrl，而大多数人不知道。
//
// 这里管清单本身（往里加、拿掉、改类型、记下哪份传到哪一步），以及「把清单
// 里的一份一份交上去」的顺序和规矩（uploadPending）。具体打哪个接口由页面
// 递进来，这里不知道也不需要知道——所以整套规矩能在没有浏览器的测试里跑。
//
// 全部是「给一张旧清单，还一张新清单」，不就地改：清单挂在 Vue 的 ref 上，换
// 引用才是它认的那种变化，而且测试里不用起浏览器。

/** 网关中转的单文件上限，和前门 nginx 的 client_max_body_size（64m）是同一个数。 */
export const MAX_UPLOAD_BYTES = 64 * 1024 * 1024

export type PendingStatus = 'waiting' | 'uploading' | 'failed'

export interface PendingUpload {
  /** 清单内唯一，给 v-for 当 key；同名文件可以有两份，不能拿文件名当 key。 */
  id: string
  file: File
  /** 这一份的类型（发票、收据……），每份各选各的。 */
  category: string
  status: PendingStatus
  /** status 为 failed 时给人看的原因，其余时候是空串。 */
  error: string
  /**
   * 文件本身已经进了存储时，它在存储里的键；还没进是空串。
   *
   * 「传上去」和「登记到单子上」是两步。第二步失败（或者干脆不知道成没成——
   * 等到超时就是这种）时文件其实已经在存储里了：下一次只补登记，不再传一遍，
   * 更不会登记出两份一样的凭证——登记了的凭证是删不掉的。
   */
  uploadedKey: string
}

export interface AddOutcome {
  list: PendingUpload[]
  /** 这次真正加进去了几份。 */
  added: number
  /** 没加进去的，按原因分开——页面要分别告诉人，「太大」和「重复」不是一回事。 */
  tooLarge: string[]
  empty: string[]
  duplicates: string[]
}

/**
 * 是不是同一份文件。
 *
 * 浏览器不给路径，能比的只有名字、大小和修改时间。三样都一样、内容却不同的
 * 两份文件理论上存在，实际上就是人把同一份选了两遍。
 */
export function sameFile(a: File, b: File): boolean {
  return a.name === b.name && a.size === b.size && a.lastModified === b.lastModified
}

/**
 * 把刚选的文件加到清单末尾。
 *
 * 不替换已有的——这正是原来那个选择框的毛病。空文件、超限的、已经在清单里
 * 的不加，各自记在返回值里由页面去说。
 */
export function addPicked(
  list: readonly PendingUpload[],
  files: readonly File[],
  category: string,
  nextId: () => string,
  maxBytes: number = MAX_UPLOAD_BYTES,
): AddOutcome {
  const outcome: AddOutcome = { list: [...list], added: 0, tooLarge: [], empty: [], duplicates: [] }
  for (const file of files) {
    if (file.size === 0) {
      outcome.empty.push(file.name)
    } else if (file.size > maxBytes) {
      outcome.tooLarge.push(file.name)
    } else if (outcome.list.some((item) => sameFile(item.file, file))) {
      outcome.duplicates.push(file.name)
    } else {
      outcome.list.push({ id: nextId(), file, category, status: 'waiting', error: '', uploadedKey: '' })
      outcome.added += 1
    }
  }
  return outcome
}

export function removePending(list: readonly PendingUpload[], id: string): PendingUpload[] {
  return list.filter((item) => item.id !== id)
}

/** 改清单里某一份的若干字段；找不到这个 id 就原样还回去。 */
export function patchPending(
  list: readonly PendingUpload[],
  id: string,
  change: Partial<Pick<PendingUpload, 'category' | 'status' | 'error' | 'uploadedKey'>>,
): PendingUpload[] {
  return list.map((item) => (item.id === id ? { ...item, ...change } : item))
}

/**
 * 新加的文件默认用什么类型：跟着清单里最后一份走。
 *
 * 人一张一张加发票的时候，不该每张都重选一遍「发票」；换了类型之后接着加的，
 * 多半还是新选的那种。清单是空的就用页面给的默认值。
 */
export function categoryForNext(list: readonly PendingUpload[], fallback: string): string {
  return list.length ? list[list.length - 1].category : fallback
}

/** 给人看的大小。不足 1 KB 的也写 1 KB——「0 KB」读起来像空文件。 */
export function formatFileSize(bytes: number): string {
  if (bytes >= 1024 * 1024) return `${(bytes / (1024 * 1024)).toFixed(1)} MB`
  return `${Math.max(1, Math.ceil(bytes / 1024))} KB`
}

/**
 * 把「其实已经登记上了」的从清单里拿掉。
 *
 * 登记那一步没等到回音时，清单里那一份记成了失败，可服务端多半已经记上了。
 * 页面重新取回单子之后拿它的键来对一遍：对上的就是交成功了的，不该再留在
 * 「没传上去」里吓人。还没进存储的（uploadedKey 为空）不受影响。
 */
export function dropRegistered(list: readonly PendingUpload[], registeredKeys: ReadonlySet<string>): PendingUpload[] {
  return list.filter((item) => !item.uploadedKey || !registeredKeys.has(item.uploadedKey))
}

/** 上传一份文件要走的三步，由页面按自己的业务接口递进来。 */
export interface UploadSteps {
  /** 这次保存开始时，单子上已经登记了哪些键。 */
  registeredKeys: ReadonlySet<string>
  /** 向业务服务要一个上传地址，连同文件将来在存储里的键。 */
  presign: (item: PendingUpload) => Promise<{ key: string; uploadUrl: string }>
  /** 把文件送到那个地址。 */
  send: (uploadUrl: string, file: File) => Promise<void>
  /** 告诉业务服务：这个键是这张单子的一份凭证。 */
  register: (item: PendingUpload, key: string) => Promise<void>
  /** 失败里给人看的那句话。 */
  reason: (err: unknown) => string
  /** 这种失败是不是「等到超时」——是的话整批停下，不让后面每一份都再等一轮。 */
  timedOut: (err: unknown) => boolean
}

/**
 * 把清单里的文件一份一份交上去，返回交完之后**还剩下**的清单。
 *
 * 每一步都把最新的清单交给 onChange，页面据此重画；传成功并登记好的那份从
 * 清单里拿掉，所以「清单空了」就是「全交上去了」。
 *
 * 一份失败之后停不停，看它失败在哪一步：
 *
 * - **要地址、登记**失败 → 整批停下。这两步是对着单子说话的（单子锁了、没
 *   权限、登录过期），换一份文件来问，答案是一样的；接着问只是把同一句报错
 *   刷满整张清单，登录过期时还会对着一个正在跳走的页面继续发请求。
 * - **送文件**失败 → 只记在这一份上，接着传下一份。这一步的失败多半是这份
 *   文件自己的事，一张发票传不上去不该拦住其余几张。等到超时的例外：那是
 *   整条路不通，停。
 *
 * 文件进了存储就把键记在那一份上（uploadedKey）。之后登记失败、下次再来时，
 * 不重新传，只补登记；要是那个键其实已经登记上了（上次是超时、不是真失败），
 * 连登记也不重复做，直接从清单里拿掉。
 */
export async function uploadPending(
  list: readonly PendingUpload[],
  steps: UploadSteps,
  onChange: (list: PendingUpload[]) => void,
): Promise<PendingUpload[]> {
  let current: PendingUpload[] = [...list]
  const update = (next: PendingUpload[]) => {
    current = next
    onChange(next)
  }

  for (const original of list) {
    update(patchPending(current, original.id, { status: 'uploading', error: '' }))
    let key = original.uploadedKey
    let stage: 'presign' | 'send' | 'register' = 'presign'
    try {
      if (!key) {
        const signed = await steps.presign(original)
        stage = 'send'
        await steps.send(signed.uploadUrl, original.file)
        key = signed.key
        update(patchPending(current, original.id, { uploadedKey: key }))
      }
      stage = 'register'
      if (!steps.registeredKeys.has(key)) {
        // 类型以清单里现在的为准：文件传上去之后、登记之前，人可能改过。
        const latest = current.find((item) => item.id === original.id) ?? original
        await steps.register(latest, key)
      }
      update(removePending(current, original.id))
    } catch (err) {
      update(patchPending(current, original.id, { status: 'failed', error: steps.reason(err) }))
      if (stage !== 'send' || steps.timedOut(err)) break
    }
  }
  return current
}
