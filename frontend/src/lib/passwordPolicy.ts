// 密码规则，和服务端那份（services/iam/internal/app/passwordpolicy.go）逐条对应。
//
// 为什么要在浏览器里再写一遍：从前人只能提交了才知道密码不合格，改一次试
// 一次。规则本身不是秘密（它拦的是弱密码，不是拦不知道规则的人），写在这边
// 就能边打边说哪几条过了。
//
// 两份实现会走散，所以有一份**共用的样例**（passwordPolicy.cases.json）：
// 这边的单测和 Go 那边的测试读的是同一个文件，谁改了规则忘了改另一边，
// 两边有一边会红。
//
// **这里没有「必须含大写字母/数字/特殊字符」那种规则**，服务端也没有，是
// 有意的：那套要求逼出来的是 P@ssw0rd1 这种全公司一个模子的密码。把住长度、
// 再把攻击者真正会试的那些挡掉，比凑字符类管用。理由写在 Go 那个文件的
// 开头。

/** 一条没过的规则。顺序和服务端一致：先短，再长，再常见，再规律，最后身份。 */
export type PasswordProblem = 'tooShort' | 'tooLong' | 'tooCommon' | 'tooSimple' | 'fromIdentity'

/** 界面上那份清单里的一条。 */
export interface PasswordRule {
  key: 'length' | 'notCommon' | 'notRepetitive' | 'notIdentity'
  ok: boolean
  /** 没过时的补充，比如密码里撞上的是哪个词。 */
  detail?: string
}

// 十位，其中中文一个字顶两位（见 Go 那边的 effectiveLength：一个汉字是几千
// 选一，一个拉丁字母是二十六选一，按字符数一刀切会专门为难写中文的人）。
const MIN_EFFECTIVE = 10
/** 再怎么"一个字顶两位"，也要有这么多个字符。 */
const MIN_RUNES = 6
/** 上限只为挡住"把一兆文本推进哈希"，不是为了限制长密码。 */
const MAX_LEN = 256
/** 身份片段短于这个就不拦：三个字母的片段和普通单词撞得太多。以字节算，同 Go。 */
const MIN_IDENTITY_PART_BYTES = 4
/** 连着这么多个相同或连续的字符就算太规律。 */
const RUN_LIMIT = 6

const COMMON_PASSWORDS = new Set([
  'password', 'passwd', 'pass', 'admin', 'administrator',
  'root', 'test', 'guest', 'user', 'login',
  'welcome', 'letmein', 'monkey', 'dragon', 'master',
  'qwerty', 'qwertyuiop', 'asdfgh', 'asdfghjkl', 'zxcvbn',
  '1qaz2wsx', '1q2w3e4r', '1q2w3e', 'qazwsx', 'qweasd',
  '123456', '1234567', '12345678', '123456789', '1234567890',
  '111111', '000000', '666666', '888888', '123123',
  'abc123', 'a123456', '123abc', 'abcd1234', 'qwe123',
  'woaini', 'woaini1314', '5201314', '1314520', 'wangyi',
  'iloveyou', 'sunshine', 'princess', 'football', 'baseball',
  'changeme', 'secret', 'default', 'temp', 'demo',
])

const KEYBOARD_WALKS = [
  '1234567890', '0987654321',
  'qwertyuiop', 'poiuytrewq',
  'asdfghjkl', 'lkjhgfdsa',
  'zxcvbnm', 'mnbvcxz',
  '1qaz2wsx3edc4rfv5tgb6yhn7ujm',
  '1q2w3e4r5t6y7u8i9o0p',
  'q1w2e3r4t5y6u7i8o9p0',
  '!@#$%^&*()',
]

/** 公共后缀谁都有，拿它拦密码只是噪音。 */
const PUBLIC_PARTS = new Set(['com', 'cn', 'net', 'org', 'edu', 'gov', 'www', 'mail', 'email'])

/** 装饰性的尾巴：「加个 1 加个叹号」那种。 */
const DECORATION = '0123456789!@#$%^&*_-.'

function chars(s: string): string[] {
  return Array.from(s)
}

/** 非 ASCII 的一个字顶两位，同 Go 的 effectiveLength。 */
function effectiveLength(runes: string[]): number {
  let n = 0
  for (const r of runes) n += (r.codePointAt(0) ?? 0) > 127 ? 2 : 1
  return n
}

function byteLength(s: string): number {
  return new TextEncoder().encode(s).length
}

function trimChars(s: string, set: string, from: 'end' | 'both'): string {
  let start = 0
  let end = s.length
  while (end > start && set.includes(s[end - 1])) end--
  if (from === 'both') {
    while (start < end && set.includes(s[start])) start++
  }
  return s.slice(start, end)
}

export function isTooShort(password: string): boolean {
  const runes = chars(password)
  return runes.length < MIN_RUNES || effectiveLength(runes) < MIN_EFFECTIVE
}

export function isTooLong(password: string): boolean {
  return chars(password).length > MAX_LEN
}

/** 常见密码，连同「脱掉装饰之后是常见密码」的那些。 */
export function isTooCommon(password: string): boolean {
  const base = password.trim().toLowerCase()
  if (COMMON_PASSWORDS.has(base)) return true
  const stripped = trimChars(base, DECORATION, 'end')
  if (stripped !== base && COMMON_PASSWORDS.has(stripped)) return true
  if (COMMON_PASSWORDS.has(trimChars(base, DECORATION, 'both'))) return true
  return walksTheKeyboard(base)
}

function walksTheKeyboard(lower: string): boolean {
  if (chars(lower).length < 6) return false
  return KEYBOARD_WALKS.some((walk) => walk.includes(lower))
}

/** 连着六个相同、或者六个顺着走的（含倒着走）。 */
export function isTooSimple(password: string): boolean {
  const runes = chars(password)
  let same = 1
  let ascending = 1
  let descending = 1
  for (let i = 1; i < runes.length; i++) {
    const cur = runes[i].codePointAt(0) ?? 0
    const prev = runes[i - 1].codePointAt(0) ?? 0
    same = cur === prev ? same + 1 : 1
    ascending = cur === prev + 1 ? ascending + 1 : 1
    descending = cur === prev - 1 ? descending + 1 : 1
    if (same >= RUN_LIMIT || ascending >= RUN_LIMIT || descending >= RUN_LIMIT) return true
  }
  return false
}

/**
 * 密码里撞上的那个身份片段，没有就是空串。
 *
 * context 里允许有 undefined 和空串：调用方往往是直接把「姓名、工号、用户名、
 * 邮箱」几个字段摊进来，而那几个字段常常还没填。让调用方各自过滤，等于把
 * 同一句 filter 抄到每个页面。
 */
export function borrowedFromIdentity(password: string, context: readonly (string | undefined)[]): string {
  const lower = password.toLowerCase()
  for (const raw of context) {
    if (!raw) continue
    for (const part of identityParts(raw)) {
      if (byteLength(part) >= MIN_IDENTITY_PART_BYTES && lower.includes(part)) return part
    }
  }
  return ''
}

function identityParts(raw: string): string[] {
  const clean = raw.trim().toLowerCase()
  if (!clean) return []
  return clean.split(/[^\p{L}\p{N}]+/u).filter((part) => part !== '' && !PUBLIC_PARTS.has(part))
}

/**
 * 第一条没过的规则，顺序和服务端一致。全过回 null。
 *
 * context 是这个人身上攻击者已经知道的那些（姓名、工号、用户名、邮箱）。
 */
export function checkPassword(password: string, context: readonly (string | undefined)[] = []): PasswordProblem | null {
  if (isTooShort(password)) return 'tooShort'
  if (isTooLong(password)) return 'tooLong'
  if (isTooCommon(password)) return 'tooCommon'
  if (isTooSimple(password)) return 'tooSimple'
  if (borrowedFromIdentity(password, context)) return 'fromIdentity'
  return null
}

/**
 * 界面上那份清单：每一条过没过，各算各的。
 *
 * 和 checkPassword 不同——那个到第一条没过就停（要和服务端报同一句话），
 * 这个要把四条都算出来，人才看得见还差哪几条。
 */
export function passwordRules(password: string, context: readonly (string | undefined)[] = []): PasswordRule[] {
  const borrowed = borrowedFromIdentity(password, context)
  return [
    { key: 'length', ok: !isTooShort(password) && !isTooLong(password) },
    { key: 'notCommon', ok: !isTooCommon(password) },
    { key: 'notRepetitive', ok: !isTooSimple(password) },
    { key: 'notIdentity', ok: borrowed === '', detail: borrowed || undefined },
  ]
}

/** 清单上一条的样子：还没开始查（○）、过了（✓）、没过（✕）。 */
export type RuleState = 'idle' | 'ok' | 'bad'

export interface RuleView {
  key: PasswordRule['key']
  state: RuleState
  /** 没过时的补充，比如密码里撞上的是哪个词。 */
  detail?: string
}

/**
 * 界面上那份清单，连同每条该画什么记号。
 *
 * 一个字都没打时四条全是 idle——不打勾也不打叉。检查从打第一个字才开始：
 * 空字符串确实「不是常见密码」，但在人还没写之前就给它打勾，看着像系统在
 * 夸一个不存在的密码（2026-09-18 老板看了第一版说的）。
 */
export function ruleStates(password: string, context: readonly (string | undefined)[] = []): RuleView[] {
  return passwordRules(password, context).map((r) => ({
    key: r.key,
    state: password === '' ? 'idle' : r.ok ? 'ok' : 'bad',
    detail: password === '' ? undefined : r.detail,
  }))
}
