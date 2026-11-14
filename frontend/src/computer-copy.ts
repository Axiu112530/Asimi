import { i18n } from './i18n';
import en from './locales/computer-center-en.json';
import zh from './locales/computer-center-zh.json';
i18n.global.mergeLocaleMessage('en-US', { computerCenter: en });
i18n.global.mergeLocaleMessage('zh-CN', { computerCenter: zh });
