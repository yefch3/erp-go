// 自动刷新失败之后，隔多久再试一次。
//
// 起因：询盘工作台挂着一个 5 秒的定时器不停重取。从前只要**失败一次**，它就
// 把自己永久关掉（`refreshSuspended = true` 之后再没有任何东西会把它翻回来），
// 人只能从「更多操作」里手动点。
//
// 这在美国几乎看不出来——2026-09-21 查生产日志，美国的客户端两周里几乎不掉。
// 在中国大陆是另一回事：同一段日志里，江苏那个 IP 五万八千次请求里有 125 次
// 是客户端等不及自己掐断的，浙江两个 IP 也各有一批。**掉线率只有 0.2%**，
// 可是每掉一次就永久废掉那个会话的自动刷新——于是 0.2% 的网络抖动被放大成
// 「一整天都看不到新数据」。修网络修不掉这个，得让它会自己爬起来。
//
// 退避而不是固定间隔重试：真出故障时（服务挂了、令牌过期）每 5 秒敲一次，
// 是在一个已经不舒服的系统上再加一份负担；而人眼前那一页反正也没有新东西。
// 前几次退得快，是因为绝大多数失败是一次性抖动，隔十几秒就好了。

// 第 n 次失败之后等多久。到顶之后一直用最后那个值。
//
// 5 秒起步和正常轮询间隔一样——第一次失败当成抖动处理，别让人察觉。
// 两分钟封顶：再长就不像「自动」刷新了，人会以为它又死了。
export const BACKOFF_MS = [5_000, 15_000, 30_000, 60_000, 120_000] as const

// 这一页当前的重试状态。
//
// 存「下一次什么时候可以试」而不是「还剩多少秒」：定时器每 5 秒醒一次，用
// 剩余秒数的话每次都要减、而漏掉一次减法就永远不会到零。存一个时间点，
// 醒来比一下大小就行，少一个能出错的地方。
export interface RefreshGate {
  // 连续失败了几次。成功一次就归零。
  failures: number
  // 早于这个时刻不要再试（epoch 毫秒）。0 = 随时可以。
  nextAttemptAt: number
}

export function idleGate(): RefreshGate {
  return { failures: 0, nextAttemptAt: 0 }
}

// 第 failures 次失败之后该等多久。failures 从 1 开始数。
export function backoffMs(failures: number): number {
  if (failures <= 0) return 0
  return BACKOFF_MS[Math.min(failures, BACKOFF_MS.length) - 1]
}

// 失败了：次数加一，把下一次的时间推后。
export function afterFailure(gate: RefreshGate, now: number): RefreshGate {
  const failures = gate.failures + 1
  return { failures, nextAttemptAt: now + backoffMs(failures) }
}

// 成功了：一切归零，下一次照常走 5 秒的节奏。
export function afterSuccess(): RefreshGate {
  return idleGate()
}

// 现在能不能试。
export function mayAttempt(gate: RefreshGate, now: number): boolean {
  return now >= gate.nextAttemptAt
}

// 要不要为这一次失败弹提示。
//
// **只在第一次失败时弹。** 之后一直退避着重试，每次都弹的话，网络不好的那
// 半小时里人会收到一串一模一样的红条——那比不提示更烦，而且会盖住真正需要
// 看的东西。恢复了就归零，所以下一轮真出事时还会再提醒一次。
export function shouldWarn(gate: RefreshGate): boolean {
  return gate.failures === 1
}
