import { app, BrowserWindow, ipcMain, type IpcMainInvokeEvent } from 'electron';
import {
  autoUpdater,
  type ProgressInfo,
  type UpdateDownloadedEvent,
  type UpdateInfo,
} from 'electron-updater';
import { APP_HOST, APP_SCHEME } from './protocol';

export type UpdateStatus =
  | { kind: 'idle' }
  | { kind: 'checking' }
  | { kind: 'available'; version: string }
  | { kind: 'downloading'; version: string; percent: number }
  | { kind: 'downloaded'; version: string }
  | { kind: 'not-available' }
  | { kind: 'error'; version: string; message: string };

const CHECK_EVERY_MS = 6 * 60 * 60 * 1000;
const log = (message?: unknown) => console.log('[update]', message);

let latest: UpdateStatus = { kind: 'idle' };
let version = '';
let staged: string | null = null;

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

export interface UpdaterHost {
  allowClose: () => void;
}

export function installUpdater(host: UpdaterHost): void {
  const supported = process.platform !== 'linux' || !!process.env.APPIMAGE;
  if (!supported)
    log('this build cannot replace itself in place: checks stay off');

  const push = (status: UpdateStatus) => {
    if (status.kind === 'downloaded') staged = status.version;
    else if (status.kind === 'available' && staged && status.version !== staged)
      staged = null;
    else if (
      staged &&
      (status.kind === 'checking' ||
        status.kind === 'not-available' ||
        status.kind === 'error')
    ) {
      return;
    }
    latest = status;
    for (const win of BrowserWindow.getAllWindows()) {
      if (!win.isDestroyed())
        win.webContents.send('desktop:update-status', status);
    }
  };

  const check = async () => {
    try {
      await autoUpdater.checkForUpdates();
    } catch (err) {
      log(`check failed: ${String(err)}`);
    }
  };

  ipcMain.handle('desktop:update:check', (e) => {
    if (!fromAppFrame(e)) throw new Error('forbidden');
    if (!supported) return { ok: false };
    void check();
    return { ok: true };
  });
  ipcMain.handle('desktop:update:status', (e) => {
    if (!fromAppFrame(e)) throw new Error('forbidden');
    return latest;
  });
  ipcMain.handle('desktop:update:restart', (e) => {
    if (!fromAppFrame(e)) throw new Error('forbidden');
    if (!supported || latest.kind !== 'downloaded') return { ok: false };
    log('restarting to install the downloaded update');
    host.allowClose();
    setImmediate(() => autoUpdater.quitAndInstall());
    return { ok: true };
  });

  app.on('browser-window-created', (_e, win) => {
    win.webContents.on('did-finish-load', () => {
      if (!win.isDestroyed())
        win.webContents.send('desktop:update-status', latest);
    });
  });

  if (!supported) return;

  autoUpdater.logger = { info: log, warn: log, error: log };
  autoUpdater.autoDownload = true;
  autoUpdater.autoInstallOnAppQuit = true;

  autoUpdater.on('checking-for-update', () => push({ kind: 'checking' }));
  autoUpdater.on('update-available', (info: UpdateInfo) => {
    version = info.version;
    push({ kind: 'available', version });
  });
  autoUpdater.on('download-progress', (p: ProgressInfo) => {
    push({ kind: 'downloading', version, percent: Math.round(p.percent) });
  });
  autoUpdater.on('update-downloaded', (info: UpdateDownloadedEvent) => {
    version = info.version;
    push({ kind: 'downloaded', version });
  });
  autoUpdater.on('update-not-available', () => {
    version = '';
    push({ kind: 'not-available' });
  });
  autoUpdater.on('error', (err: Error) =>
    push({ kind: 'error', version, message: err.message }),
  );

  void check();
  const timer = setInterval(() => void check(), CHECK_EVERY_MS);
  timer.unref();
  app.on('before-quit', () => clearInterval(timer));
}
