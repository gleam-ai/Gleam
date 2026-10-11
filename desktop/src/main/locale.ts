import { app } from 'electron';

export type Lang = 'zh' | 'en';

export function systemLang(): Lang {
  try {
    const loc = app.getLocale() || '';
    return /^zh/i.test(loc) ? 'zh' : 'en';
  } catch {
    return 'en';
  }
}
