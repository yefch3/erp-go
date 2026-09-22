// 「下载全部」：一封信的附件一次拿走。
//
// 从前这颗按钮走的是服务端打包——请求 /inbound-mails/<信>/attachments/download，
// 服务器现场把几个文件压成一个 zip 发回来。**现在不打包了**：拿到的就是原来
// 那几个文件，名字、格式都和发信人给的一样，不用先解压再找。
//
// 顺带解决了 zip 的两个老毛病：中文名在某些解压工具里变乱码（zip 里的文件名
// 编码历来是一笔糊涂账），以及大附件要在服务器上先攒齐才能开始发。
//
// 两条路，取决于浏览器给不给「选文件夹」：
//
//   给（Chrome / Edge）→ 弹一次文件夹选择，几个文件一起写进去。
//                        这就是「另存为」，人自己决定放哪儿。
//   不给（Firefox / Safari）→ 逐个触发普通下载，落在浏览器的默认下载目录。
//
// 选文件夹用的是 File System Access API（showDirectoryPicker）。它必须由
// 点击直接触发——不能 await 了别的东西再调，浏览器会以"不是用户手势"拒掉。
// 所以下面 saveAllAttachments 的第一件事就是弹选择框，取文件在那之后。

export interface SavableFile {
  fileName: string
  downloadUrl?: string
}

// 同一封信里两个附件重名是常事（邮件客户端给内嵌图片起的名字就那么几个，
// image.png 能出现好几次）。写进同一个文件夹时后一个会盖掉前一个——**而且
// 不报错**，人拿到的文件比列表里少，还不知道少了哪个。
//
// 所以在写之前就把名字错开：第二个 image.png 变成 image (2).png，扩展名留在
// 最后（改扩展名会让系统认不出该用什么打开）。这和 Windows、macOS 复制同名
// 文件时的做法一致，不用解释。
//
// 大小写不敏感：Windows 和 macOS 的文件系统默认都认为 IMAGE.PNG 和 image.png
// 是同一个名字，按大小写区分的话，在那两个系统上照样会互相覆盖。
export function uniqueFileNames(names: string[]): string[] {
  const used = new Set<string>()
  return names.map((raw) => {
    const name = raw.trim() || '附件'
    const dot = name.lastIndexOf('.')
    // 开头就是点的（.gitignore）不算扩展名，整串都是名字。
    const stem = dot > 0 ? name.slice(0, dot) : name
    const ext = dot > 0 ? name.slice(dot) : ''
    let candidate = name
    let n = 1
    while (used.has(candidate.toLowerCase())) {
      n += 1
      candidate = `${stem} (${n})${ext}`
    }
    used.add(candidate.toLowerCase())
    return candidate
  })
}

// 这个浏览器让不让人自己选文件夹。
//
// 只认 window 上真有这个函数，不按浏览器名字判断：按名字判断的代码会在下一个
// 支持它的浏览器上继续走降级路径，而那时没人会想起来回头改。
export function canPickDirectory(win: unknown = globalThis): boolean {
  return typeof (win as { showDirectoryPicker?: unknown })?.showDirectoryPicker === 'function'
}

// 能拿的那些。没有 downloadUrl 的是「原件已经不在了」（超大附件从没存过、
// 或者过期清掉了），列表上那一行本来就是灰的，这里也跳过——为它弹一个错误
// 不如安静地少一个文件，反正屏幕上已经说了它拿不到。
export function downloadableOnly<T extends SavableFile>(files: T[]): T[] {
  return files.filter((f) => Boolean(f.downloadUrl))
}

export interface SaveOutcome {
  // 真写下去几个。
  saved: number
  // 取不到的那几个的文件名。一个失败不该连累其余，所以是列表不是异常。
  failed: string[]
  // 人在选文件夹那一步按了取消。这不是错误，是改主意——调用方据此什么都
  // 不提示。分不出这一档的话，取消一次会弹一句"下载失败"。
  cancelled: boolean
}

type Fetcher = (url: string) => Promise<Blob>

const fetchBlob: Fetcher = async (url) => {
  const resp = await fetch(url)
  if (!resp.ok) throw new Error(String(resp.status))
  return resp.blob()
}

/**
 * 把这些附件一次存下来。
 *
 * deps 全部可注入，是为了能在没有浏览器的测试里跑：pickDirectory 假装人选了
 * 一个文件夹，fetchBlob 假装网络，saveOne 假装降级路径。
 */
export async function saveAllAttachments(
  files: SavableFile[],
  deps: {
    pickDirectory?: () => Promise<FileSystemDirectoryHandle | null>
    fetchBlob?: Fetcher
    // 降级路径：把一个 blob 交给浏览器按这个名字下载。
    saveOne?: (blob: Blob, fileName: string) => void
  } = {},
): Promise<SaveOutcome> {
  const wanted = downloadableOnly(files)
  if (!wanted.length) return { saved: 0, failed: [], cancelled: false }

  const names = uniqueFileNames(wanted.map((f) => f.fileName))
  const get = deps.fetchBlob ?? fetchBlob

  // **先问文件夹，再取文件。** 顺序不能反：选文件夹必须由点击直接触发，
  // 中间 await 过别的东西之后浏览器就不认这个手势了。
  let dir: FileSystemDirectoryHandle | null = null
  if (deps.pickDirectory) {
    try {
      dir = await deps.pickDirectory()
    } catch {
      // 人按了取消。DOMException AbortError 走这里。
      return { saved: 0, failed: [], cancelled: true }
    }
    if (!dir) return { saved: 0, failed: [], cancelled: true }
  }

  let saved = 0
  const failed: string[] = []
  for (let i = 0; i < wanted.length; i++) {
    const file = wanted[i]
    const name = names[i]
    try {
      const blob = await get(file.downloadUrl as string)
      if (dir) {
        const handle = await dir.getFileHandle(name, { create: true })
        const writable = await handle.createWritable()
        await writable.write(blob)
        await writable.close()
      } else {
        deps.saveOne?.(blob, name)
      }
      saved += 1
    } catch {
      // 逐个尽力：一个取不到不该连累其余。名字收集起来，一次说清。
      failed.push(file.fileName)
    }
  }
  return { saved, failed, cancelled: false }
}
