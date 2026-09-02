// 字节数变成人看的「121.9 KB」。列表、附件、详情三处用的是同一把尺，
// 否则同一封信在三个地方会写出三个不同的数。
export function humanSize(bytes: number | string | undefined): string {
  const n = Number(bytes)
  if (!n || n < 0) return '0 B'
  if (n < 1024) return `${n} B`
  if (n < 1024 * 1024) return `${Math.round(n / 1024)} KB`
  return `${(n / 1024 / 1024).toFixed(1)} MB`
}
