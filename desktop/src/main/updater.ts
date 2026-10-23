import { app, BrowserWindow, ipcMain, type IpcMainInvokeEvent } from 'electron';
import {
  autoUpdater,
  type ProgressInfo,
  type UpdateDownloadedEvent,
  type UpdateInfo,
} from 'electron-updater';
import { spawn } from 'node:child_process';
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
  // Stops the sidecar before the installer overwrites files on disk.
  quitForUpdate: () => Promise<void>;
  // The local engine, which proxies the release feed around github.com.
  upstream: () => { addr: string; token: string } | null;
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
    if (!configureFeed()) {
      push({ kind: 'error', version, message: '本机服务还没就绪，请稍后再试' });
      return;
    }
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
  ipcMain.handle('desktop:update:download', async (e) => {
    if (!fromAppFrame(e)) throw new Error('forbidden');
    if (!supported || latest.kind !== 'available') return { ok: false };
    try {
      await autoUpdater.downloadUpdate();
      return { ok: true };
    } catch (err) {
      log(`download failed: ${String(err)}`);
      return { ok: false, error: String(err) };
    }
  });
  ipcMain.handle('desktop:update:restart', async (e) => {
    if (!fromAppFrame(e)) throw new Error('forbidden');
    if (!supported || latest.kind !== 'downloaded') return { ok: false };

    const installerPath = (autoUpdater as any).installerPath;
    if (!installerPath) {
      log('update downloaded but installer path not available');
      return { ok: false };
    }

    // Stop the sidecar before launching: the installer overwrites
    // resources/bin/gleam.exe, and an open handle leaves a corrupted file.
    await host.quitForUpdate();
    if (process.platform === 'win32') {
      // Keep our own spawn instead of quitAndInstall() (its elevate.exe path
      // corrupted the sidecar on NSIS builds), but pass --updated so NSIS
      // treats this as an in-place update rather than a fresh install.
      const child = spawn(installerPath, ['--updated'], {
        detached: true,
        stdio: 'ignore',
        windowsHide: false,
      });
      child.on('error', (err) => log(`installer spawn failed: ${String(err)}`));
      if (!child.pid) {
        log('failed to start installer');
        return { ok: false, error: 'failed to start installer' };
      }
      child.unref();
      log(`installer launched: ${installerPath}`);
      app.quit();
      return { ok: true };
    }
    host.allowClose();
    autoUpdater.quitAndInstall(false, true);
    return { ok: true };
  });

  app.on('browser-window-created', (_e, win) => {
    win.webContents.on('did-finish-load', () => {
      if (!win.isDestroyed())
        win.webContents.send('desktop:update-status', latest);
    });
  });

  if (!supported) return;

  // The feed lives behind the local engine, which reads the release through
  // api.github.com. electron-updater's GitHub provider would hit github.com,
  // and that host is reset on some networks while the API stays reachable.
  let feed = '';
  const configureFeed = (): boolean => {
    const up = host.upstream();
    if (!up) return false;
    const next = `${up.addr}\n${up.token}`;
    if (next !== feed) {
      feed = next;
      autoUpdater.setFeedURL({
        provider: 'generic',
        url: `http://${up.addr}/api/update/desktop`,
      });
      autoUpdater.requestHeaders = { 'X-Gleam-Token': up.token };
      log(`feed: local engine (${up.addr})`);
    }
    return true;
  };

  autoUpdater.logger = { info: log, warn: log, error: log };
  // Only download after the user clicks update: silently pulling 265 MB at
  // startup wastes bandwidth and disk.
  autoUpdater.autoDownload = false;
  autoUpdater.autoInstallOnAppQuit = false; // User must explicitly click restart
  // The feed has no per-version blockmaps, so a differential download would
  // diff against the wrong one; always fetch the full installer.
  autoUpdater.disableDifferentialDownload = true;

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
