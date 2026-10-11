import {
  app,
  BrowserWindow,
  dialog,
  ipcMain,
  shell,
  type IpcMainInvokeEvent,
  type Session,
  type WebContents,
} from 'electron';
import { APP_HOST, APP_ORIGIN, APP_SCHEME } from './protocol';
import { getShortcut, setShortcut } from './shortcut';

function isAppURL(raw: string): boolean {
  try {
    const u = new URL(raw);
    return u.protocol === `${APP_SCHEME}:` && u.host === APP_HOST;
  } catch {
    return false;
  }
}

function isExternalHTTP(raw: string): boolean {
  try {
    const u = new URL(raw);
    return u.protocol === 'http:' || u.protocol === 'https:';
  } catch {
    return false;
  }
}

export function hardenContents(contents: WebContents): void {
  const guardNav = (e: Electron.Event, url: string) => {
    if (!isAppURL(url)) {
      e.preventDefault();
      console.warn(`[security] blocked navigation to ${url}`);
    }
  };
  contents.on('will-navigate', guardNav);
  contents.on('will-redirect', guardNav);

  contents.setWindowOpenHandler(({ url }) => {
    if (isExternalHTTP(url)) void shell.openExternal(url);
    else console.warn(`[security] blocked window.open(${url})`);
    return { action: 'deny' };
  });

  contents.on('will-attach-webview', (e) => {
    e.preventDefault();
    console.warn('[security] blocked <webview>');
  });
}

export function hardenSession(ses: Session): void {
  ses.setPermissionRequestHandler((_wc, permission, callback, details) => {
    const ok =
      permission === 'clipboard-sanitized-write' &&
      isAppURL(details.requestingUrl);
    if (!ok)
      console.warn(
        `[security] denied permission ${permission} for ${details.requestingUrl}`,
      );
    callback(ok);
  });
  ses.setPermissionCheckHandler((_wc, permission, requestingOrigin) => {
    return (
      permission === 'clipboard-sanitized-write' &&
      requestingOrigin === APP_ORIGIN
    );
  });
  ses.on('will-download', (e) => e.preventDefault());
}

export const IPC_CHANNELS = [
  'desktop:info',
  'desktop:edit',
  'desktop:view',
  'desktop:window',
  'desktop:windowState',
  'desktop:openExternal',
  'desktop:capture',
  'desktop:newWindow',
  'desktop:showOpenDialog',
  'desktop:globalShortcut',
  'desktop:update:check',
  'desktop:update:status',
  'desktop:update:restart',
  'desktop:tray:sync',
] as const;
export const IPC_EVENTS = [
  'desktop:window-state',
  'desktop:update-status',
  'desktop:tray-action',
] as const;

const EDIT_ACTIONS = [
  'undo',
  'redo',
  'cut',
  'copy',
  'paste',
  'pasteAndMatchStyle',
  'delete',
  'selectAll',
] as const;
type EditAction = (typeof EDIT_ACTIONS)[number];
const VIEW_ACTIONS = [
  'reload',
  'forceReload',
  'resetZoom',
  'zoomIn',
  'zoomOut',
] as const;
const WINDOW_ACTIONS = ['minimize', 'maximize', 'close', 'fullscreen'] as const;
const ZOOM_STEP = 0.5;
const ZOOM_MIN = -3;
const ZOOM_MAX = 4;
const CAPTURE_MAX_WIDTH = 1600;

function fromAppFrame(e: IpcMainInvokeEvent): boolean {
  const frame = e.senderFrame;
  return !!frame && frame.parent === null && isAppURL(frame.url);
}

function oneOf<T extends string>(list: readonly T[], v: unknown): v is T {
  return typeof v === 'string' && (list as readonly string[]).includes(v);
}

function senderWindow(e: IpcMainInvokeEvent): BrowserWindow {
  const win = BrowserWindow.fromWebContents(e.sender);
  if (!win) throw new Error('no window');
  return win;
}

export function windowState(win: BrowserWindow): {
  maximized: boolean;
  fullscreen: boolean;
} {
  return { maximized: win.isMaximized(), fullscreen: win.isFullScreen() };
}

export function forwardWindowState(win: BrowserWindow): void {
  const push = () => {
    if (!win.isDestroyed())
      win.webContents.send('desktop:window-state', windowState(win));
  };
  for (const ev of [
    'maximize',
    'unmaximize',
    'enter-full-screen',
    'leave-full-screen',
    'restore',
  ] as const) {
    win.on(ev as 'maximize', push);
  }
}

export function installIpc(
  info: () => Record<string, unknown>,
  newWindow: () => void,
): void {
  const guard =
    <A extends unknown[], R>(fn: (e: IpcMainInvokeEvent, ...args: A) => R) =>
    (e: IpcMainInvokeEvent, ...args: A): R => {
      if (!fromAppFrame(e)) throw new Error('forbidden');
      return fn(e, ...args);
    };

  ipcMain.handle(
    'desktop:info',
    guard(() => ({
      ...info(),
      appVersion: app.getVersion(),
      platform: process.platform,
      arch: process.arch,
      electron: process.versions.electron,
      chrome: process.versions.chrome,
    })),
  );

  ipcMain.handle(
    'desktop:edit',
    guard((e, action: unknown) => {
      if (!oneOf(EDIT_ACTIONS, action)) throw new Error('bad edit action');
      const wc = e.sender;
      const map: Record<EditAction, () => void> = {
        undo: () => wc.undo(),
        redo: () => wc.redo(),
        cut: () => wc.cut(),
        copy: () => wc.copy(),
        paste: () => wc.paste(),
        pasteAndMatchStyle: () => wc.pasteAndMatchStyle(),
        delete: () => wc.delete(),
        selectAll: () => wc.selectAll(),
      };
      map[action]();
    }),
  );

  ipcMain.handle(
    'desktop:view',
    guard((e, action: unknown) => {
      if (!oneOf(VIEW_ACTIONS, action)) throw new Error('bad view action');
      const wc = e.sender;
      const z = wc.getZoomLevel();
      switch (action) {
        case 'reload':
          wc.reload();
          break;
        case 'forceReload':
          wc.reloadIgnoringCache();
          break;
        case 'resetZoom':
          wc.setZoomLevel(0);
          break;
        case 'zoomIn':
          wc.setZoomLevel(Math.min(ZOOM_MAX, z + ZOOM_STEP));
          break;
        case 'zoomOut':
          wc.setZoomLevel(Math.max(ZOOM_MIN, z - ZOOM_STEP));
          break;
      }
    }),
  );

  ipcMain.handle(
    'desktop:window',
    guard((e, action: unknown) => {
      if (!oneOf(WINDOW_ACTIONS, action)) throw new Error('bad window action');
      const win = senderWindow(e);
      switch (action) {
        case 'minimize':
          win.minimize();
          break;
        case 'maximize':
          if (win.isMaximized()) win.unmaximize();
          else win.maximize();
          break;
        case 'close':
          win.close();
          break;
        case 'fullscreen':
          win.setFullScreen(!win.isFullScreen());
          break;
      }
      return windowState(win);
    }),
  );

  ipcMain.handle(
    'desktop:windowState',
    guard((e) => windowState(senderWindow(e))),
  );

  ipcMain.handle(
    'desktop:openExternal',
    guard(async (_e, url: unknown) => {
      if (typeof url !== 'string' || !isExternalHTTP(url))
        throw new Error('only http(s) URLs can be opened externally');
      await shell.openExternal(url);
      return true;
    }),
  );

  ipcMain.handle(
    'desktop:capture',
    guard(async (e) => {
      let img = await e.sender.capturePage();
      const { width } = img.getSize();
      if (width > CAPTURE_MAX_WIDTH)
        img = img.resize({ width: CAPTURE_MAX_WIDTH, quality: 'good' });
      return img.toDataURL();
    }),
  );

  ipcMain.handle(
    'desktop:newWindow',
    guard(() => {
      newWindow();
    }),
  );

  ipcMain.handle(
    'desktop:globalShortcut',
    guard((_e, action: unknown, accelerator: unknown) => {
      if (action === 'get') return { ok: true, accelerator: getShortcut() };
      if (action === 'set') {
        if (typeof accelerator !== 'string') throw new Error('bad accelerator');
        return setShortcut(accelerator);
      }
      throw new Error('bad shortcut action');
    }),
  );

  ipcMain.handle(
    'desktop:showOpenDialog',
    guard(async (e, options: unknown) => {
      const win = senderWindow(e);
      const opts =
        typeof options === 'object' && options !== null ? options : {};
      const result = await dialog.showOpenDialog(
        win,
        opts as Electron.OpenDialogOptions,
      );
      return result.canceled ? [] : result.filePaths;
    }),
  );
}
