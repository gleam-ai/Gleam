import {
  app,
  BrowserWindow,
  dialog,
  Menu,
  Tray,
  ipcMain,
  nativeImage,
  type IpcMainInvokeEvent,
  type MenuItemConstructorOptions,
  type NativeImage,
} from 'electron';
import { existsSync, writeFileSync } from 'node:fs';
import { join } from 'node:path';
import { APP_HOST, APP_SCHEME } from './protocol';

const RECENT_TOP = 5;
const RECENT_MORE = 10;
const TITLE_MAX = 40;

export type TrayLang = 'zh' | 'en';
export interface TrayRecent {
  id: string;
  title: string;
}
export type TrayAction = {
  action: 'new-chat' | 'open-convo' | 'settings';
  id?: string;
};

const TEXT = {
  zh: {
    recent: '最近',
    more: '更多',
    newChat: '新建聊天',
    open: '打开 Gleam',
    settings: '设置',
    quit: '退出 Gleam',
    quitTitle: '确认退出',
    quitMessage: '确定要退出 Gleam 吗？',
    quitConfirm: '退出',
    quitCancel: '取消',
  },
  en: {
    recent: 'Recent',
    more: 'More',
    newChat: 'New chat',
    open: 'Open Gleam',
    settings: 'Settings',
    quit: 'Quit Gleam',
    quitTitle: 'Confirm Quit',
    quitMessage: 'Are you sure you want to quit Gleam?',
    quitConfirm: 'Quit',
    quitCancel: 'Cancel',
  },
} as const;

const HINT = {
  zh: {
    title: 'Gleam 仍在后台运行',
    content:
      '关闭窗口只是把它收起来。要退出，请用托盘图标右键菜单里的「退出 Gleam」。',
  },
  en: {
    title: 'Gleam is still running',
    content:
      'Closing the window only hides it. To quit, use "Quit Gleam" in the tray menu.',
  },
} as const;

let tray: Tray | null = null;
let lang: TrayLang = 'zh';
let recents: TrayRecent[] = [];

function fromAppFrame(e: IpcMainInvokeEvent): boolean {
  const frame = e.senderFrame;
  if (!frame || frame.parent !== null) return false;
  try {
    const u = new URL(frame.url);
    return u.protocol === `${APP_SCHEME}:` && u.host === APP_HOST;
  } catch {
    return false;
  }
}

function trayIcon(): NativeImage {
  const assets = join(__dirname, '..', '..', '..', 'assets');
  const res = process.resourcesPath ?? '';
  const candidates =
    process.platform === 'win32'
      ? [join(res, 'tray', 'gleam.ico'), join(assets, 'gleam.ico')]
      : [
          join(res, 'tray', 'gleam-32.png'),
          join(assets, 'icons', 'gleam-32.png'),
        ];
  for (const p of candidates) {
    if (p && existsSync(p)) {
      const img = nativeImage.createFromPath(p);
      if (!img.isEmpty()) return img;
    }
  }
  console.warn(
    `[tray] 没找到托盘图标，先用空图（图标位置仍然在，只是透明的）：${candidates.join(' | ')}`,
  );
  return nativeImage.createEmpty();
}

function target(): BrowserWindow | null {
  const focused = BrowserWindow.getFocusedWindow();
  if (focused && !focused.isDestroyed()) return focused;
  return BrowserWindow.getAllWindows().find((w) => !w.isDestroyed()) ?? null;
}

function reveal(): void {
  const w = target();
  if (!w) return;
  if (w.isMinimized()) w.restore();
  w.show();
  w.focus();
}

export function dispatchTrayAction(a: TrayAction): void {
  reveal();
  const w = target();
  if (w) w.webContents.send('desktop:tray-action', a);
}

function label(title: string, fallback: string): string {
  const t = (title || '').replace(/\s+/g, ' ').trim() || fallback;
  return t.length > TITLE_MAX ? `${t.slice(0, TITLE_MAX - 1)}…` : t;
}

async function confirmQuit(): Promise<void> {
  const t = TEXT[lang];
  const w = target();
  const result = w
    ? await dialog.showMessageBox(w, {
        type: 'question',
        title: t.quitTitle,
        message: t.quitMessage,
        buttons: [t.quitCancel, t.quitConfirm],
        defaultId: 0,
        cancelId: 0,
      })
    : await dialog.showMessageBox({
        type: 'question',
        title: t.quitTitle,
        message: t.quitMessage,
        buttons: [t.quitCancel, t.quitConfirm],
        defaultId: 0,
        cancelId: 0,
      });
  if (result.response === 1) app.quit();
}

export function describeTrayMenu(): string {
  const t = TEXT[lang];
  const head = recents
    .slice(0, RECENT_TOP)
    .map((r) => JSON.stringify(label(r.title, r.id.slice(0, 8))));
  const rest = recents.length - RECENT_TOP;
  const recent =
    recents.length === 0
      ? '(无最近会话)'
      : `${t.recent}[${head.join(' / ')}${rest > 0 ? ` + ${t.more}(${Math.min(rest, RECENT_MORE)})` : ''}]`;
  const parts = recents.length === 0 ? [] : [recent];
  parts.push(t.newChat, t.open, t.settings, t.quit);
  return parts.join(' | ');
}

function buildMenu(): Menu {
  const t = TEXT[lang];
  const convoItem = (r: TrayRecent): MenuItemConstructorOptions => ({
    label: label(r.title, r.id.slice(0, 8)),
    click: () => dispatchTrayAction({ action: 'open-convo', id: r.id }),
  });

  const items: MenuItemConstructorOptions[] = [];
  if (recents.length > 0) {
    const sub = recents.slice(0, RECENT_TOP).map(convoItem);
    const rest = recents.slice(RECENT_TOP, RECENT_TOP + RECENT_MORE);
    if (rest.length)
      sub.push(
        { type: 'separator' },
        { label: t.more, submenu: rest.map(convoItem) },
      );
    items.push({ label: t.recent, submenu: sub });
    items.push({ type: 'separator' });
  }

  items.push(
    {
      label: t.newChat,
      click: () => dispatchTrayAction({ action: 'new-chat' }),
    },
    { label: t.open, click: reveal },
    {
      label: t.settings,
      click: () => dispatchTrayAction({ action: 'settings' }),
    },
    { type: 'separator' },
    { label: t.quit, click: () => void confirmQuit() },
  );
  return Menu.buildFromTemplate(items);
}

function applySync(state: unknown): void {
  const s = (typeof state === 'object' && state !== null ? state : {}) as {
    lang?: unknown;
    recents?: unknown;
  };
  if (s.lang === 'en' || s.lang === 'zh') lang = s.lang;
  if (Array.isArray(s.recents)) {
    recents = s.recents
      .filter(
        (r): r is TrayRecent =>
          !!r &&
          typeof r === 'object' &&
          typeof (r as TrayRecent).id === 'string' &&
          (r as TrayRecent).id !== '' &&
          typeof (r as TrayRecent).title === 'string',
      )
      .slice(0, RECENT_TOP + RECENT_MORE)
      .map((r) => ({ id: r.id, title: r.title }));
  }
  tray?.setContextMenu(buildMenu());
  console.log(`[tray] 菜单已更新：lang=${lang} recents=${recents.length}`);
  if (!app.isPackaged) console.log(`[tray] 内容：${describeTrayMenu()}`);
}

export function installTray(): void {
  if (tray) return;
  const icon = trayIcon();
  tray = new Tray(icon);
  tray.setToolTip('Gleam');
  tray.setContextMenu(buildMenu());
  tray.on('click', () => {
    const w = target();
    if (!w) return;
    if (w.isVisible() && !w.isMinimized()) w.hide();
    else reveal();
  });

  ipcMain.handle('desktop:tray:sync', (e, state: unknown) => {
    if (!fromAppFrame(e)) throw new Error('forbidden');
    applySync(state);
    return { ok: true };
  });
  console.log(
    `[tray] 已创建：icon=${icon.isEmpty() ? '(empty)' : 'ok'} 后端语言默认=${lang}`,
  );
}

export function trayHintOnce(): void {
  if (!tray) return;
  const flag = join(app.getPath('userData'), 'tray-hint.json');
  if (existsSync(flag)) return;
  try {
    writeFileSync(flag, JSON.stringify({ shown: new Date().toISOString() }));
  } catch {
    return;
  }
  if (process.platform !== 'win32') return;
  const t = HINT[lang];
  try {
    tray.displayBalloon({ title: t.title, content: t.content });
  } catch {}
}
