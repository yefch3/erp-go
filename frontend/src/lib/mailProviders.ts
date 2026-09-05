// 邮件服务商清单：员工加邮箱时挑一个，服务器不用他填。
//
// **这份清单只管显示。** 真正连哪台服务器由服务端按 code 查它自己那张表
// （services/mail/internal/app/mailproviders.go）决定——主机名如果由前端发
// 上去，任何登录了的员工都能让邮件服务带着凭据去连任意 host:port。所以这里
// 的 code 必须和服务端那张表一字不差；对不上时服务端会当场报「认不出这个
// 邮件服务商」，而不是猜一个连上去。
//
// 之所以从 MailHostDialog.vue 搬出来：那个对话框是**管理员**配「一家公司
// 一套」服务器的地方，只有 iam:role:write 看得见。现在每个员工都要能给
// 自己加信箱，而且可能是别家服务商的（263 的公司里绑 Gmail），预设必须
// 出现在员工的表单上。
//
// 放在 lib/ 而不是留在 .vue 里，是因为 .vue 组件在这个仓库里没有任何单元
// 测试手段（没装 @vue/test-utils），留在里面就是零覆盖。
export interface MailProvider {
  /** 和服务端 mailproviders.go 共用的代号。 */
  code: string
  /** 界面上显示的名字。不走 i18n 动态键——见文件末尾那段注释。 */
  label: string
  /** 一句话告诉员工去哪儿拿授权码。这是这份清单最有用的部分。 */
  hint: string
  /** 认得出来的地址后缀，只用来在员工输入地址后自动选中。 */
  domains?: readonly string[]
  /** true = 这家已经关掉了密码登录，只能走它自己的授权。 */
}

export const MAIL_PROVIDERS: readonly MailProvider[] = [
  {
    code: 'p263',
    label: '263 企业邮',
    hint: '在 263 邮箱网页版「设置 → 客户端设置」里开启 IMAP，密码就是邮箱密码',
  },
  {
    code: 'tencent',
    label: '腾讯企业邮',
    hint: '在企业邮网页版「设置 → 收发信设置」里开启 IMAP，并生成一个客户端专用密码',
  },
  {
    code: 'ali',
    label: '阿里企业邮',
    hint: '在阿里邮箱「设置 → 账户与安全」里开启 IMAP，用邮箱密码登录',
  },
  {
    code: 'netease',
    label: '网易企业邮',
    hint: '在企业邮网页版「设置 → 客户端设置」里开启 IMAP，并生成客户端授权码',
  },
  {
    code: 'gmail',
    label: 'Gmail',
    hint: '推荐直接用下面的「用 Google 登录」。要填密码的话，需要先开两步验证，再生成一个「应用专用密码」——账号密码本身登不上',
    domains: ['gmail.com', 'googlemail.com'],
  },
  {
    code: 'qq',
    label: 'QQ 邮箱',
    hint: '在 QQ 邮箱「设置 → 账户」里开启 IMAP，会给你一串授权码；填授权码，不是 QQ 密码',
    domains: ['qq.com', 'vip.qq.com', 'foxmail.com'],
  },
  {
    code: 'netease163',
    label: '163 邮箱',
    hint: '在 163 邮箱「设置 → POP3/SMTP/IMAP」里开启 IMAP，会给你一串授权码；填授权码，不是登录密码',
    domains: ['163.com'],
  },
  {
    code: 'netease126',
    label: '126 邮箱',
    hint: '在 126 邮箱「设置 → POP3/SMTP/IMAP」里开启 IMAP，会给你一串授权码；填授权码，不是登录密码',
    domains: ['126.com'],
  },
  {
    code: 'sina',
    label: '新浪邮箱',
    hint: '在新浪邮箱「设置 → 客户端 POP/IMAP/SMTP」里开启 IMAP，并生成独立密码',
    domains: ['sina.com', 'sina.cn'],
  },
] as const

/** 「都不是上面这些」：服务器由员工自己填。和服务端的 ProviderCodeOther 对应。 */
export const PROVIDER_OTHER = 'other'

/**
 * 按地址后缀猜服务商，猜不出来返回 null。
 *
 * 只对个人邮箱有用。企业邮用各家公司自己的域名，从 me@sunrise.com 看不出
 * 它托管在腾讯还是 263——那种情况留空，服务端会落回这家公司自己配的那套。
 */
export function providerForAddress(email: string): MailProvider | null {
  const at = email.lastIndexOf('@')
  if (at < 0) return null
  const domain = email.slice(at + 1).trim().toLowerCase()
  if (!domain) return null
  return MAIL_PROVIDERS.find((p) => p.domains?.includes(domain)) ?? null
}

/** 按代号取，认不得返回 null。 */
export function providerByCode(code: string): MailProvider | null {
  const want = code.trim().toLowerCase()
  return MAIL_PROVIDERS.find((p) => p.code === want) ?? null
}

// 名字和提示语直接写在这里，不走 i18n 的动态键。
//
// 原来的写法是 t(`mailbox.presetNames.${p.key}`)，而 locales.test.ts 只扫
// 字面量形式的调用，模板字符串拼出来的键扫不到——加一家服务商却漏了词条，
// 测试全绿，界面上直接显示 `mailbox.presetNames.outlook` 这串键名。这个仓库
// 真出过这种事故。服务商名本来也不翻译（「263 企业邮」在英文界面里还是那
// 几个字），所以写死比补三份词条更诚实。
