import { app, BrowserWindow, dialog, session } from 'electron';
import { existsSync } from 'node:fs';
import { homedir } from 'node:os';
import { join, resolve } from 'node:path';
import { systemLang } from './locale';
import { APP_ORIGIN, installAppProtocol, registerAppScheme } from './protocol';
import { hardenContents, hardenSession, installIpc } from './security';
import { Sidecar, type SidecarSpec } from './sidecar';
import { createMainWindow } from './window';
import { installGlobalShortcut } from './shortcut';
import { probeRuntimes, runtimeBinDirs } from './runtimes';
import { Splash } from './splash';
import { installTray, dispatchTrayAction, trayHintOnce } from './tray';
import { installUpdater } from './updater';

const t0 = Date.now();
const log = (msg: string) =>
  console.log(`[desktop +${Date.now() - t0}ms] ${msg}`);

const SPLASH_HOLD_SECONDS = 5;

if (process.env.GLEAM_DESKTOP_USER_DATA)
  app.setPath('userData', resolve(process.env.GLEAM_DESKTOP_USER_DATA));

registerAppScheme();
app.enableSandbox();

function sidecarSpec(): SidecarSpec {
  const exe = process.platform === 'win32' ? 'gleam.exe' : 'gleam';
  const args = (process.env.GLEAM_SIDECAR_ARGS || '')
    .split(/\s+/)
    .filter(Boolean);
  const extraPath = runtimeBinDirs();
  if (process.env.GLEAM_SIDECAR_BIN) {
    return {
      bin: resolve(process.env.GLEAM_SIDECAR_BIN),
      args,
      cwd: process.cwd(),
      extraPath,
    };
  }
  if (app.isPackaged) {
    return {
      bin: join(process.resourcesPath, 'bin', exe),
      args,
      cwd: homedir(),
      extraPath,
    };
  }
  const desktopDir = resolve(__dirname, '..', '..');
  const bin = join(
    desktopDir,
    '.sidecar',
    `${process.platform}-${process.arch}`,
    exe,
  );
  return { bin, args, cwd: resolve(desktopDir, '..'), extraPath };
}

if (!app.requestSingleInstanceLock()) {
  console.log(
    '[desktop] another instance is running; handing over and exiting',
  );
  app.exit(0);
} else {
  run();
}

function run(): void {
  let win: BrowserWindow | null = null;
  let sidecar: Sidecar | null = null;
  let quitting = false;
  let allowWindowClose = false;

  app.on('second-instance', () => {
    log('second-instance: focusing the existing window');
    if (!win) return;
    if (win.isMinimized()) win.restore();
    win.show();
    win.focus();
  });

  app.on('web-contents-created', (_e, contents) => hardenContents(contents));

  app.on('window-all-closed', () => app.quit());

  app.on('before-quit', (e) => {
    if (quitting || !sidecar?.running) return;
    e.preventDefault();
    quitting = true;
    log('quit: stopping sidecar');
    void sidecar.stop().then((how) => {
      log(`sidecar stopped (${how})`);
      app.quit();
    });
  });

  for (const sig of ['SIGINT', 'SIGTERM'] as const)
    process.on(sig, () => app.quit());

  const w = Number(process.env.GLEAM_DESKTOP_WIDTH) || undefined;
  const h = Number(process.env.GLEAM_DESKTOP_HEIGHT) || undefined;

  app.whenReady().then(async () => {
    hardenSession(session.defaultSession);
    installIpc(
      () => ({ sidecarVersion: sidecar?.ready?.version ?? null }),
      () => {
        const extra = createMainWindow({ width: w, height: h });
        void extra.loadURL(`${APP_ORIGIN}/`);
      },
    );
    installAppProtocol(() =>
      sidecar?.ready
        ? { addr: sidecar.ready.addr, token: sidecar.token }
        : null,
    );

    const spec = sidecarSpec();
    if (!existsSync(spec.bin)) {
      dialog.showErrorBox(
        'Gleam',
        `Backend binary not found:\n${spec.bin}\n\nRun "npm run build:sidecar" first.`,
      );
      app.exit(1);
      return;
    }

    const lang = systemLang();
    let splash: Splash | null = null;
    if (!process.env.GLEAM_NO_SPLASH) {
      splash = new Splash(lang);
      splash.render({
        status:
          lang === 'zh' ? '正在检查内置运行时…' : 'Checking bundled runtimes…',
        items: [],
        total: 3,
        holdSeconds: 0,
      });
    }
    const probes = await probeRuntimes(lang);
    const broken = probes.filter((p) => p.bundled && !p.ok);
    for (const p of probes)
      log(`runtime ${p.name}: ${p.ok ? 'ok' : 'missing'} ${p.version || ''}`);
    const holdSeconds = broken.length ? SPLASH_HOLD_SECONDS : 0;
    splash?.render({
      status: broken.length
        ? lang === 'zh'
          ? `${broken.length} 项没就绪——可以继续，用到它的功能会提示`
          : `${broken.length} item(s) not ready — you can continue; features that need them will tell you`
        : lang === 'zh'
          ? '环境已就绪，正在启动…'
          : 'Environment ready — starting…',
      items: probes,
      total: probes.length,
      holdSeconds,
    });

    sidecar = new Sidecar(spec);
    sidecar.on('unexpected-exit', (code, signal) => {
      log(`sidecar exited unexpectedly (code=${code}, signal=${signal})`);
      if (quitting) return;
      quitting = true;
      dialog.showErrorBox(
        'Gleam',
        `The Gleam backend stopped unexpectedly (code=${code}, signal=${signal}).`,
      );
      app.quit();
    });
    try {
      const ready = await sidecar.start();
      log(
        `sidecar ready pid=${ready.pid} addr=${ready.addr} version=${ready.version}`,
      );
    } catch (err) {
      log(`sidecar failed to start: ${String(err)}`);
      dialog.showErrorBox(
        'Gleam',
        `The Gleam backend failed to start:\n${String(err)}`,
      );
      app.exit(1);
      return;
    }

    win = createMainWindow({ width: w, height: h });
    installGlobalShortcut(win);
    installTray();
    win.on('closed', () => (win = null));
    win.on('close', (e) => {
      if (quitting || allowWindowClose) return;
      e.preventDefault();
      win?.hide();
      trayHintOnce();
    });
    win.webContents.once('did-finish-load', () =>
      log('renderer did-finish-load'),
    );
    await win.loadURL(`${APP_ORIGIN}/`);
    if (holdSeconds > 0)
      await new Promise((r) => setTimeout(r, holdSeconds * 1000));
    splash?.close();

    if (app.isPackaged)
      installUpdater({
        allowClose: () => {
          allowWindowClose = true;
        },
      });

    const demo = (process.env.GLEAM_TRAY_ACTION || '')
      .split(',')
      .map((s) => s.trim())
      .filter(Boolean);
    demo.forEach((spec, i) => {
      const [action, id] = spec.split(':');
      if (
        action !== 'new-chat' &&
        action !== 'settings' &&
        action !== 'open-convo'
      ) {
        log(`tray demo: 不认的动作 ${spec}`);
        return;
      }
      setTimeout(
        () => {
          log(`tray demo: ${spec}`);
          dispatchTrayAction(id ? { action, id } : { action });
        },
        6000 * (i + 1),
      );
    });
  });
}
