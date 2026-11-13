import { createI18n } from 'vue-i18n';
import en from './locales/en-US.json';
import zh from './locales/zh-CN.json';
import computerEn from './locales/computer-suggestion.en-US.json';
import computerZh from './locales/computer-suggestion.zh-CN.json';
import cognitionEn from './locales/cognition.en-US.json';
import cognitionZh from './locales/cognition.zh-CN.json';

export const i18n = createI18n({ legacy: false, locale: 'en-US', fallbackLocale: 'en-US', missingWarn: false, fallbackWarn: false, messages: {
  'en-US': { ...en, ...computerEn, ...cognitionEn, errors: { ...en.errors, ...computerEn.errors, ...cognitionEn.errors } },
  'zh-CN': { ...zh, ...computerZh, ...cognitionZh, errors: { ...zh.errors, ...computerZh.errors, ...cognitionZh.errors } },
} });
export const t = (key: string, params: Record<string,string|number> = {}): string => i18n.global.t(key, params);
export function known(key: string, fallback = 'common.unknown'): string { return i18n.global.te(key, 'en-US') ? t(key) : t(fallback); }
export function date(value: string): string { const d = new Date(value); return Number.isNaN(d.valueOf()) ? t('common.unknown') : new Intl.DateTimeFormat(i18n.global.locale.value, { dateStyle:'medium',timeStyle:'short' }).format(d); }
export const number = (value: number): string => new Intl.NumberFormat(i18n.global.locale.value).format(value);
