// 只给测试用。每个现代浏览器的 DecompressionStream 都认 'deflate-raw'；
// 跑测试的那个精简版 Node 不认。拿 zlib 顶上，让读 .xlsx 的那条路（zip
// 成员都是 deflate 压的）在测试里也真的走一遍。
//
// 副作用模块：import 一下就装好了。
import { inflateRawSync } from 'node:zlib'

try {
  void new DecompressionStream('deflate-raw')
} catch {
  const Native = globalThis.DecompressionStream
  globalThis.DecompressionStream = class {
    constructor(format: string) {
      if (format !== 'deflate-raw') return new Native(format as 'deflate') as TransformStream<Uint8Array, Uint8Array>
      const chunks: Uint8Array[] = []
      return new TransformStream({
        transform(chunk, controller) {
          chunks.push(chunk as Uint8Array)
          void controller
        },
        flush(controller) {
          const all = new Uint8Array(chunks.reduce((sum, chunk) => sum + chunk.length, 0))
          let at = 0
          for (const chunk of chunks) {
            all.set(chunk, at)
            at += chunk.length
          }
          controller.enqueue(inflateRawSync(all))
        },
      }) as TransformStream<Uint8Array, Uint8Array>
    }
  } as typeof DecompressionStream
}
