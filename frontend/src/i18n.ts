import { createI18n } from 'vue-i18n'
import zh from './locales/zh'
import en from './locales/en'
import es from './locales/es'

export type Locale = 'zh' | 'en' | 'es'

export const LOCALE_LABELS: Record<Locale, string> = {
  zh: '中文',
  en: 'English',
  es: 'Español',
}

export const i18n = createI18n({
  legacy: false,
  locale: (localStorage.getItem('locale') as Locale) ?? 'zh',
  fallbackLocale: 'en',
  messages: { zh, en, es },
})

export function setLocale(l: Locale) {
  i18n.global.locale.value = l
  localStorage.setItem('locale', l)
}
