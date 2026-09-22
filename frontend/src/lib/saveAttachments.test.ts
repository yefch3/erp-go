import { describe, expect, it, vi } from 'vitest'

import {
  canPickDirectory,
  downloadableOnly,
  saveAllAttachments,
  uniqueFileNames,
} from './saveAttachments'

describe('uniqueFileNames', () => {
  it('不重名的原样留着', () => {
    expect(uniqueFileNames(['合同.pdf', '装箱单.xlsx'])).toEqual(['合同.pdf', '装箱单.xlsx'])
  })

  // 这条是整个改动最要紧的一条：从前打成 zip，zip 内部允许重名；现在几个文件
  // 写进同一个文件夹，后一个会**静默盖掉**前一个——人拿到的比列表里少，还不
  // 知道少了哪个。邮件客户端给内嵌图片起的名字就那么几个，image.png 常常出现
  // 好几次，所以这不是边角情况。
  it('重名的往后排号，扩展名留在最后', () => {
    expect(uniqueFileNames(['image.png', 'image.png', 'image.png'])).toEqual([
      'image.png',
      'image (2).png',
      'image (3).png',
    ])
  })

  it('已经带序号的也不会撞上', () => {
    expect(uniqueFileNames(['a.pdf', 'a (2).pdf', 'a.pdf'])).toEqual([
      'a.pdf',
      'a (2).pdf',
      'a (3).pdf',
    ])
  })

  // Windows 和 macOS 的文件系统默认不区分大小写，按大小写当成两个名字的话，
  // 在那两个系统上照样互相覆盖。
  it('大小写不同也算重名', () => {
    expect(uniqueFileNames(['Report.PDF', 'report.pdf'])).toEqual(['Report.PDF', 'report (2).pdf'])
  })

  it('没有扩展名的照样排号', () => {
    expect(uniqueFileNames(['README', 'README'])).toEqual(['README', 'README (2)'])
  })

  // 点开头的是隐藏文件，不是扩展名：改成 "(2).gitignore" 会让它变成另一种东西。
  it('点开头的整串都算名字', () => {
    expect(uniqueFileNames(['.gitignore', '.gitignore'])).toEqual(['.gitignore', '.gitignore (2)'])
  })

  it('空名字兜底', () => {
    expect(uniqueFileNames(['', '  '])).toEqual(['附件', '附件 (2)'])
  })
})

describe('canPickDirectory', () => {
  it('有这个函数就算支持', () => {
    expect(canPickDirectory({ showDirectoryPicker: () => {} })).toBe(true)
  })

  it('没有就算不支持', () => {
    expect(canPickDirectory({})).toBe(false)
  })

  // 按浏览器名字判断的代码会在下一个支持它的浏览器上继续走降级路径，
  // 而那时没人会想起来回头改。这里只认函数在不在。
  it('不是函数不算', () => {
    expect(canPickDirectory({ showDirectoryPicker: true })).toBe(false)
  })
})

describe('downloadableOnly', () => {
  it('原件不在了的跳过', () => {
    const files = [
      { fileName: 'a.pdf', downloadUrl: 'u1' },
      { fileName: 'b.pdf' },
      { fileName: 'c.pdf', downloadUrl: '' },
    ]
    expect(downloadableOnly(files).map((f) => f.fileName)).toEqual(['a.pdf'])
  })
})

// 假的文件夹句柄：记下每个文件被写了什么。
function fakeDirectory() {
  const written = new Map<string, string>()
  const handle = {
    getFileHandle: async (name: string) => ({
      createWritable: async () => ({
        write: async (blob: Blob) => {
          written.set(name, await blob.text())
        },
        close: async () => {},
      }),
    }),
  }
  return { handle: handle as unknown as FileSystemDirectoryHandle, written }
}

describe('saveAllAttachments 选了文件夹', () => {
  it('每个文件各写一份，重名的自动排号', async () => {
    const { handle, written } = fakeDirectory()
    const out = await saveAllAttachments(
      [
        { fileName: 'image.png', downloadUrl: 'u1' },
        { fileName: 'image.png', downloadUrl: 'u2' },
      ],
      {
        pickDirectory: async () => handle,
        fetchBlob: async (url) => new Blob([`内容-${url}`]),
      },
    )
    expect(out).toEqual({ saved: 2, failed: [], cancelled: false })
    expect([...written.keys()]).toEqual(['image.png', 'image (2).png'])
    expect(written.get('image.png')).toBe('内容-u1')
    expect(written.get('image (2).png')).toBe('内容-u2')
  })

  // 一个取不到不该连累其余——和 signDownloads 那边同一个取舍。
  it('一个取不到，其余照样写下去', async () => {
    const { handle, written } = fakeDirectory()
    const out = await saveAllAttachments(
      [
        { fileName: '好的.pdf', downloadUrl: 'ok' },
        { fileName: '坏的.pdf', downloadUrl: 'bad' },
        { fileName: '也好.pdf', downloadUrl: 'ok2' },
      ],
      {
        pickDirectory: async () => handle,
        fetchBlob: async (url) => {
          if (url === 'bad') throw new Error('404')
          return new Blob(['x'])
        },
      },
    )
    expect(out.saved).toBe(2)
    expect(out.failed).toEqual(['坏的.pdf'])
    expect(out.cancelled).toBe(false)
    expect([...written.keys()]).toEqual(['好的.pdf', '也好.pdf'])
  })

  // 按取消是改主意，不是出错。分不出这一档的话，取消一次会弹一句"下载失败"。
  it('人按了取消：什么都不做，也不算失败', async () => {
    const out = await saveAllAttachments([{ fileName: 'a.pdf', downloadUrl: 'u' }], {
      pickDirectory: async () => {
        throw new DOMException('The user aborted a request.', 'AbortError')
      },
      fetchBlob: async () => new Blob(['x']),
    })
    expect(out).toEqual({ saved: 0, failed: [], cancelled: true })
  })

  it('选择框回了空也算取消', async () => {
    const out = await saveAllAttachments([{ fileName: 'a.pdf', downloadUrl: 'u' }], {
      pickDirectory: async () => null,
    })
    expect(out.cancelled).toBe(true)
  })

  // 顺序不能反：选文件夹必须由点击直接触发，中间 await 过别的东西之后浏览器
  // 就不认这个手势了。这条钉住"先问文件夹，再取文件"。
  it('先弹文件夹选择，再去取文件', async () => {
    const order: string[] = []
    const { handle } = fakeDirectory()
    await saveAllAttachments([{ fileName: 'a.pdf', downloadUrl: 'u' }], {
      pickDirectory: async () => {
        order.push('选文件夹')
        return handle
      },
      fetchBlob: async () => {
        order.push('取文件')
        return new Blob(['x'])
      },
    })
    expect(order).toEqual(['选文件夹', '取文件'])
  })
})

describe('saveAllAttachments 没有文件夹选择（Firefox / Safari）', () => {
  it('逐个交给浏览器下载，名字照样错开', async () => {
    const saved: string[] = []
    const out = await saveAllAttachments(
      [
        { fileName: 'image.png', downloadUrl: 'u1' },
        { fileName: 'image.png', downloadUrl: 'u2' },
      ],
      {
        fetchBlob: async () => new Blob(['x']),
        saveOne: (_blob, name) => saved.push(name),
      },
    )
    expect(out).toEqual({ saved: 2, failed: [], cancelled: false })
    expect(saved).toEqual(['image.png', 'image (2).png'])
  })

  it('没有可下的就什么都不做', async () => {
    const saveOne = vi.fn()
    const out = await saveAllAttachments([{ fileName: 'a.pdf' }], { saveOne })
    expect(out).toEqual({ saved: 0, failed: [], cancelled: false })
    expect(saveOne).not.toHaveBeenCalled()
  })
})
