import { createI18n } from 'vue-i18n'

export type Locale = 'zh' | 'en' | 'es'

export const LOCALE_LABELS: Record<Locale, string> = {
  zh: '中文',
  en: 'English',
  es: 'Español',
}

// 三种语言各自一个动态 import，于是它们各成一个分片，只有真正切到的那一种
// 才会被下载。
//
// 从前三份一起 import，全部打进主包：三个文件源码加起来 480 KB，而每个人
// 只看其中一份——另外两份是每次首屏都要下载、解析、常驻内存的死重。
const LOADERS: Record<Locale, () => Promise<{ default: Record<string, unknown> }>> = {
  zh: () => import('./locales/zh'),
  en: () => import('./locales/en'),
  es: () => import('./locales/es'),
}

function isLocale(v: string | null): v is Locale {
  return v === 'zh' || v === 'en' || v === 'es'
}

/** 这次打开该用哪种语言。认不出的（手改过的 localStorage）回中文。 */
export function savedLocale(): Locale {
  const v = localStorage.getItem('locale')
  return isLocale(v) ? v : 'zh'
}

// fallbackLocale 指向自己，也就是**不跨语言兜底**。
//
// 这不是省事，是那条兜底本来就永远不会触发：locales.test.ts 强制三份词条的
// 键集完全一致（「en 和中文一一对应」那两条），缺一个键测试当场变红。留着
// 跨语言兜底的唯一后果，是切到英文时还要把中文那 160 KB 一起下下来，为一件
// 测试保证不会发生的事付钱。
export const i18n = createI18n({
  legacy: false,
  locale: savedLocale(),
  fallbackLocale: savedLocale(),
  messages: {},
})

/** 把某种语言的词条装进来；装过就不再装。 */
export async function loadLocale(l: Locale): Promise<void> {
  if (i18n.global.availableLocales.includes(l)) return
  const mod = await LOADERS[l]()
  i18n.global.setLocaleMessage(l, mod.default as never)
}

/**
 * 挂载之前把当前这种语言装好。
 *
 * 必须 await：词条还没到就挂载，第一帧会把键名画到按钮上（`emails.reply`
 * 那样），然后闪一下变成文案。
 */
export async function bootLocale(): Promise<void> {
  await loadLocale(savedLocale())
}

export async function setLocale(l: Locale): Promise<void> {
  await loadLocale(l)
  i18n.global.locale.value = l
  i18n.global.fallbackLocale.value = l
  localStorage.setItem('locale', l)
}
