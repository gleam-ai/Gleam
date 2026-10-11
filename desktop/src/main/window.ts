import { BrowserWindow, nativeTheme } from 'electron';
import { existsSync } from 'node:fs';
import { join } from 'node:path';
import { forwardWindowState } from './security';

function resolveAppIcon(): string | undefined {
  if (process.platform === 'darwin') return undefined;
  const candidates = [
    join(process.resourcesPath ?? '', 'icon.png'),
    join(__dirname, '..', '..', '..', 'assets', 'icons', 'gleam-256.png'),
  ];
  return candidates.find((p) => p && existsSync(p));
}

export function createMainWindow(
  opts: { width?: number; height?: number } = {},
): BrowserWindow {
  const isMac = process.platform === 'darwin';
  const icon = resolveAppIcon();
  console.log(`[desktop] window icon: ${icon ?? '(built-in)'}`);
  const win = new BrowserWindow({
    width: opts.width ?? 1280,
    height: opts.height ?? 840,
    ...(icon ? { icon } : {}),
    minWidth: 720,
    minHeight: 480,
    show: false,
    title: 'Gleam',
    backgroundColor: nativeTheme.shouldUseDarkColors ? '#161615' : '#f5f5f2',
    autoHideMenuBar: true,
    ...(isMac
      ? {
          titleBarStyle: 'hidden' as const,
          trafficLightPosition: { x: 14, y: 14 },
        }
      : { frame: false }),
    webPreferences: {
      preload: join(__dirname, '..', 'preload', 'index.js'),
      contextIsolation: true,
      sandbox: true,
      nodeIntegration: false,
      nodeIntegrationInSubFrames: false,
      webviewTag: false,
      spellcheck: false,
    },
  });
  win.setMenu(null);
  forwardWindowState(win);
  win.once('ready-to-show', () => win.show());
  return win;
}
