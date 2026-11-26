// Real-Electron probe for the shell <-> renderer IPC path. It loads the real
// main bundle, the real sidecar and the real UI, then inspects the renderer to
// answer one question per run: at which layer does an IPC call stop?
//
// Usage (from the repo root):
//   desktop/node_modules/.bin/electron desktop/scripts/probe-electron-ipc.cjs
// Environment:
//   GLEAM_PROBE_OUT   output JSON path (default: OS temp dir)
//   GLEAM_PROBE_URL   page to probe (default: the app root)
//   GLEAM_PROBE_ENTRY main bundle to load (default: dist/main/index.js)
//   GLEAM_PROBE_REAL_OPEN  one URL to hand to the real system browser at the
//                     end of the run; records whether Windows accepted it.
//
// shell.openExternal is replaced with a recorder so no browser window opens.
// The probe never touches the user's profile: userData and GLEAM_DATA_DIR are
// redirected to a throwaway directory.
const { app, shell } = require('electron');
const { mkdirSync, mkdtempSync, rmSync, writeFileSync } = require('node:fs');
const { tmpdir } = require('node:os');
const { join, resolve } = require('node:path');

const started = Date.now();
const record = { events: [], probes: {}, result: 'unfinished' };
const push = (kind, detail) =>
  record.events.push({ t: Date.now() - started, kind, ...detail });
for (const level of ['log', 'warn', 'error']) {
  const original = console[level].bind(console);
  console[level] = (...args) => {
    push('console', { level, text: args.map(String).join(' ') });
    original(...args);
  };
}

const scratch = mkdtempSync(join(tmpdir(), 'gleam-probe-'));
process.env.GLEAM_DESKTOP_USER_DATA = join(scratch, 'userData');
process.env.GLEAM_DATA_DIR = join(scratch, 'data');
process.env.GLEAM_NO_SPLASH = '1';
mkdirSync(process.env.GLEAM_DESKTOP_USER_DATA, { recursive: true });
mkdirSync(process.env.GLEAM_DATA_DIR, { recursive: true });

const opened = [];
const realOpenExternal = shell.openExternal.bind(shell);
shell.openExternal = (url, options) => {
  opened.push(String(url));
  return Promise.resolve();
};
push('meta', { openExternalStubbed: shell.openExternal !== realOpenExternal });

const finish = async (code) => {
  if (process.env.GLEAM_PROBE_REAL_OPEN) {
    const url = process.env.GLEAM_PROBE_REAL_OPEN;
    record.realOpen = { url, state: 'pending' };
    await Promise.race([
      realOpenExternal(url).then(
        () => (record.realOpen = { url, state: 'resolved' }),
        (err) =>
          (record.realOpen = { url, state: 'rejected', error: String(err) }),
      ),
      new Promise((r) =>
        setTimeout(() => {
          record.realOpen = { url, state: 'no-settle-in-10s' };
          r();
        }, 10_000),
      ),
    ]);
  }
  record.opened = opened;
  record.result = code === 0 ? 'done' : 'timeout';
  const out = process.env.GLEAM_PROBE_OUT || join(scratch, 'probe-result.json');
  writeFileSync(out, JSON.stringify(record, null, 2));
  // Chromium still holds files under userData while this process lives, so the
  // scratch directory is left behind on purpose; it lives in the OS temp dir.
  app.exit(code);
};
setTimeout(() => finish(2), 90_000).unref();

const PROBE_URL = process.env.GLEAM_PROBE_URL || 'app://gleam/';
const RENDERER_PROBE = `(() => {
  const d = window.gleamDesktop;
  const run = (label, fn) =>
    Promise.resolve()
      .then(fn)
      .then(
        (v) => ({ label, ok: true, value: v === undefined ? null : v }),
        (e) => ({ label, ok: false, error: String(e && e.message ? e.message : e) }),
      );
  const out = {
    href: location.href,
    isDesktop: !!(d && d.isDesktop),
    keys: d ? Object.keys(d) : [],
  };
  const calls = [];
  if (d) {
    calls.push(run('desktop:info', () => d.info()));
    calls.push(run('desktop:windowState', () => d.windowState()));
    if (typeof d.openExternal === 'function') {
      calls.push(run('openExternal(https)', () => d.openExternal('https://example.com/probe-https')));
      calls.push(run('openExternal(http)', () => d.openExternal('http://example.com/probe-http')));
      calls.push(run('openExternal(bad-scheme)', () => d.openExternal('file:///C:/Windows/win.ini')));
    }
  }
  const link = (attrs) => {
    const a = document.createElement('a');
    a.href = 'https://example.com/probe-anchor';
    a.textContent = 'probe';
    for (const k of Object.keys(attrs)) a.setAttribute(k, attrs[k]);
    a.style.cssText = 'position:fixed;left:0;top:0;z-index:2147483647';
    document.body.appendChild(a);
    return a;
  };
  return Promise.all(calls).then((results) => {
    out.ipc = results;
    out.dom = {
      bpExternal: !!document.getElementById('bp-external'),
      anchorsToExternal: [...document.querySelectorAll('a[href^="http"]')]
        .slice(0, 5)
        .map((a) => ({ href: a.href, target: a.target, rel: a.rel })),
    };
    const plain = link({});
    plain.click();
    return new Promise((r) => setTimeout(r, 300)).then(() => {
      out.afterPlainClick = location.href;
      plain.remove();
      const blank = link({ target: '_blank' });
      blank.click();
      return new Promise((r) => setTimeout(r, 300)).then(() => {
        blank.remove();
        out.afterBlankClick = location.href;
        return out;
      });
    });
  });
})()`;

app.on('web-contents-created', (_e, contents) => {
  push('main', { text: 'web-contents-created' });
  for (const name of [
    'did-start-loading',
    'did-finish-load',
    'did-fail-load',
    'did-start-navigation',
    'preload-error',
    'console-message',
  ]) {
    contents.on(name, (...args) =>
      push('contents', {
        name,
        args: args
          .slice(0, 4)
          .map((a) =>
            typeof a === 'object' ? String((a && a.message) || a) : a,
          ),
      }),
    );
  }
  contents.once('did-finish-load', () => {
    push('main', { text: 'did-finish-load' });
    setTimeout(async () => {
      try {
        const before = opened.length;
        const out = await contents.executeJavaScript(RENDERER_PROBE, true);
        record.probes[PROBE_URL] = out;
        record.openedByRenderer = opened.slice(before);
        push('main', { text: 'probe complete' });
        finish(0);
      } catch (err) {
        record.error = String(err);
        push('main', { text: `probe failed: ${String(err)}` });
        finish(3);
      }
    }, 1500);
  });
});

process.on('uncaughtException', (err) => {
  record.uncaught = String(err && err.stack ? err.stack : err);
  finish(4);
});

// GLEAM_PROBE_ENTRY points the probe at another main bundle, for example the
// one inside an installed app.asar, so two builds can be compared in the same
// real renderer.
const entry =
  process.env.GLEAM_PROBE_ENTRY ||
  resolve(__dirname, '..', 'dist', 'main', 'index.js');
push('meta', { entry, resourcesPath: process.resourcesPath });
require(entry);
