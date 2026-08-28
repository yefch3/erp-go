// 后端传过来的 id 什么时候算「没有」。
//
// 这是一个踩过的坑，而且它不响：网关用 protojson 编码响应，并且开着
// EmitUnpopulated（services/gateway/internal/httpapi/server.go:960），
// 于是**每一个 int64 字段都会出现在 JSON 里，而且是字符串**——没值的时候
// 是 "0"，不是 0，也不是 null，更不是缺省。
//
// 而 JavaScript 里 "0" 是**真值**。所以这一行看起来天经地义的代码：
//
//     v-if="!row.matchedPaymentId"      // 「还没匹配付款单的时候显示」
//
// 对**每一行**都是 false —— 包括一行都没匹配过的。银行流水页就是这么坏的：
// 「改归属」按钮在任何一行上都不出现，而「取消匹配」在任何一行上都出现，
// 尽管旁边的「匹配状态」那一列老老实实写着「—」。那一列读的是
// matchedPaymentNo（string 类型，空就是空串，判断正确），两列读同一件事、
// 读出两个答案，页面自己跟自己打架。
//
// 更坏的是它不报错、不打日志、界面看上去也是完整的——只是那个按钮不在。
//
// 判断 id 有没有值一律走这里，不要直接判真假。
// 数字字段（金额、数量）不适用，那些请继续用 Number()。

/** 这个 id 指向一条真实记录吗。"" / "0" / undefined 都算没有。 */
export function hasId(v: string | number | null | undefined): boolean {
  if (v === null || v === undefined) return false
  const s = String(v).trim()
  return s !== '' && s !== '0'
}

/** hasId 的反面。写成两个函数而不是让调用处加 !，是因为 `!hasId(x)` 和
 *  `!x` 长得太像，而后者正是这里要根除的写法。 */
export function noId(v: string | number | null | undefined): boolean {
  return !hasId(v)
}
