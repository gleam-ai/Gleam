'use strict';

// Smoke coverage for the renderer -> preload -> ipcMain -> shell IPC boundary.
// This layer had no test before; upstream analysis of the "open link" P0
// converged here and could not be narrowed further without one.

const test = require('node:test');
const assert = require('node:assert');
const fs = require('node:fs');
const os = require('node:os');
const path = require('node:path');
const { EventEmitter } = require('node:events');
const { loadTsModule } = require('./helpers/electron-harness');

const MAIN = path.join(__dirname, '..', 'src', 'main');
const PRELOAD = path.join(__dirname, '..', 'src', 'preload', 'index.ts');
const COMPUTER_USE_MAIN = path.join(
  __dirname,
  '..',
  'src',
  'computer_use',
  'main.ts',
);
const COMPUTER_USE_SERVER = path.join(
  __dirname,
  '..',
  'src',
  'computer_use',
  'server.ts',
);
const APP_URL = 'app://gleam/';
const MAIN_SOURCES = ['security.ts', 'updater.ts', 'tray.ts'];
const HANDLE_RE = /ipcMain\.handle\(\s*'([^']+)'/g;

function readMain(name) {
  return fs.readFileSync(path.join(MAIN, name), 'utf8');
}

function literalChannels(text) {
  return [...text.matchAll(HANDLE_RE)].map((m) => m[1]);
}

function createMainHarness({ shellOpen = async () => undefined } = {}) {
  const handlers = new Map();
  const calls = { shell: [], openDialog: [] };
  const win = {
    isMaximized: () => false,
    isFullScreen: () => false,
    isDestroyed: () => false,
    minimize() {},
    maximize() {},
    unmaximize() {},
    close() {},
    setFullScreen() {},
  };
  const wc = new EventEmitter();
  Object.assign(wc, {
    undo() {},
    redo() {},
    cut() {},
    copy() {},
    paste() {},
    pasteAndMatchStyle() {},
    delete() {},
    selectAll() {},
    reload() {},
    reloadIgnoringCache() {},
    getZoomLevel: () => 0,
    setZoomLevel() {},
    capturePage: async () => ({
      getSize: () => ({ width: 800 }),
      toDataURL: () => 'data:,',
    }),
    send() {},
  });
  let windowOpenHandler = null;
  wc.setWindowOpenHandler = (fn) => {
    windowOpenHandler = fn;
  };

  const session = new EventEmitter();
  let permissionRequestHandler = null;
  let permissionCheckHandler = null;
  session.setPermissionRequestHandler = (fn) => {
    permissionRequestHandler = fn;
  };
  session.setPermissionCheckHandler = (fn) => {
    permissionCheckHandler = fn;
  };

  const electron = {
    app: {
      isPackaged: true,
      getVersion: () => '0.0.39',
      getPath: () => path.join(os.tmpdir(), 'gleam-ipc-smoke'),
      platform: process.platform,
    },
    BrowserWindow: {
      fromWebContents: () => win,
      getAllWindows: () => [win],
    },
    ipcMain: { handle: (name, handler) => handlers.set(name, handler) },
    shell: {
      openExternal: (url) => {
        calls.shell.push(url);
        return shellOpen(url);
      },
    },
    dialog: {
      showOpenDialog: async (_win, opts) => {
        calls.openDialog.push(opts);
        return { canceled: false, filePaths: ['C:/tmp/a.txt'] };
      },
    },
    systemPreferences: { askForMediaAccess: async () => true },
    globalShortcut: { register: () => true, unregister: () => {} },
    net: {},
    protocol: {},
  };

  const security = loadTsModule(path.join(MAIN, 'security.ts'), {
    electron,
    reset: [path.join(MAIN, 'protocol.ts'), path.join(MAIN, 'shortcut.ts')],
  });
  security.installIpc(
    () => ({ sidecarVersion: '0.0.39' }),
    () => {},
  );
  security.hardenContents(wc);
  security.hardenSession(session);

  const appEvent = () => ({
    senderFrame: { parent: null, url: APP_URL },
    sender: wc,
  });
  const invokeFrom = (name, event, ...args) => {
    const handler = handlers.get(name);
    assert.ok(handler, `no ipcMain handler for ${name}`);
    return handler(event, ...args);
  };
  const invoke = (name, ...args) => invokeFrom(name, appEvent(), ...args);

  return {
    security,
    handlers,
    calls,
    wc,
    win,
    session,
    electron,
    appEvent,
    invoke,
    invokeFrom,
    get windowOpenHandler() {
      return windowOpenHandler;
    },
    permissionRequest: (permission, cb, details) =>
      permissionRequestHandler(null, permission, cb, details),
    permissionCheck: (permission, origin, details = {}) =>
      permissionCheckHandler(null, permission, origin, details),
  };
}

function createPreloadHarness() {
  const invoked = [];
  const listened = [];
  const bridge = {};
  const electron = {
    contextBridge: {
      exposeInMainWorld: (name, api) => {
        bridge[name] = api;
      },
    },
    ipcRenderer: {
      invoke: (channel, ...args) => {
        invoked.push({ channel, args });
        return Promise.resolve(undefined);
      },
      on: (channel) => {
        listened.push(channel);
        return () => {};
      },
      removeListener: () => {},
    },
  };
  loadTsModule(PRELOAD, { electron });
  return { bridge, invoked, listened };
}

async function exerciseEveryPreloadApi(api) {
  await api.info();
  await api.edit('copy');
  await api.view('reload');
  await api.window('minimize');
  await api.windowState();
  await api.onWindowState(() => {});
  await api.openExternal('https://example.com');
  await api.capture();
  await api.newWindow();
  await api.showOpenDialog({});
  await api.globalShortcut('get');
  await api.update.check();
  await api.update.status();
  await api.update.download();
  await api.update.restart();
  await api.update.onStatus(() => {});
  await api.tray.sync({ lang: 'zh', recents: [] });
  await api.tray.onAction(() => {});
}

test('IPC_CHANNELS owns every desktop:* handler exactly once', () => {
  const { security } = createMainHarness();
  const handled = MAIN_SOURCES.flatMap((name) =>
    literalChannels(readMain(name)),
  );
  assert.equal(
    new Set(handled).size,
    handled.length,
    'a channel must not be registered twice',
  );
  assert.deepEqual(handled.sort(), [...security.IPC_CHANNELS].sort());
});

test('every preload invoke channel and event belongs to the declared contract', async () => {
  const { security } = createMainHarness();
  const { bridge, invoked, listened } = createPreloadHarness();
  assert.ok(bridge.gleamDesktop, 'preload must expose gleamDesktop');
  await exerciseEveryPreloadApi(bridge.gleamDesktop);

  assert.deepEqual(
    [...new Set(invoked.map((i) => i.channel))].sort(),
    [...security.IPC_CHANNELS].sort(),
  );
  assert.deepEqual(
    [...new Set(listened)].sort(),
    [...security.IPC_EVENTS].sort(),
  );
});

test('renderer -> preload -> ipcMain -> shell.openExternal smoke', async () => {
  const main = createMainHarness();
  const { bridge, invoked } = createPreloadHarness();
  const urls = [
    'http://example.com/a?b=1',
    'https://example.com/x',
    'mailto:hi@example.com',
  ];

  for (const url of urls) {
    invoked.length = 0;
    await bridge.gleamDesktop.openExternal(url);
    assert.equal(invoked.length, 1);
    assert.equal(invoked[0].channel, 'desktop:openExternal');
    assert.deepEqual(invoked[0].args, [url]);
    // Bridge the recorded IPC call into the real main-process handler.
    assert.equal(
      await main.invoke(invoked[0].channel, ...invoked[0].args),
      true,
    );
  }
  assert.deepEqual(main.calls.shell, urls);
});

test('openExternal rejects non-allowlisted URLs without touching the shell', async () => {
  const main = createMainHarness();
  const bad = [
    'file:///C:/Windows/notepad.exe',
    'javascript:alert(1)',
    'data:text/html,<script>1</script>',
    'ftp://example.com/x',
    'gleam://open',
    '//example.com/x',
    '',
    'not a url',
    42,
    null,
    undefined,
    {},
    ['https://example.com'],
  ];
  for (const value of bad) {
    await assert.rejects(
      () => main.invoke('desktop:openExternal', value),
      /only http\(s\)\/mailto URLs can be opened externally/,
    );
  }
  assert.equal(main.calls.shell.length, 0);
});

test('frame guard rejects non-app frames before the URL whitelist', async () => {
  const main = createMainHarness();
  const events = [
    { senderFrame: { parent: {}, url: APP_URL }, sender: main.wc },
    {
      senderFrame: { parent: null, url: 'https://evil.example/' },
      sender: main.wc,
    },
    { senderFrame: null, sender: main.wc },
    { sender: main.wc },
  ];
  // The frame guard is a synchronous wrapper, so its rejection arrives as a
  // throw; the URL whitelist lives inside the async handler and rejects later.
  for (const event of events) {
    assert.throws(
      () =>
        main.invokeFrom('desktop:openExternal', event, 'https://example.com'),
      /forbidden/,
    );
  }
  assert.equal(main.calls.shell.length, 0);
});

test('shell.openExternal failures surface as renderer-side rejections', async () => {
  const main = createMainHarness({
    shellOpen: async () => {
      throw new Error('Windows cannot open this URL');
    },
  });
  await assert.rejects(
    () => main.invoke('desktop:openExternal', 'https://example.com'),
    /Windows cannot open this URL/,
  );
  assert.deepEqual(main.calls.shell, ['https://example.com']);
});

test('window.open bypass uses the same whitelist and always denies in-app', () => {
  const main = createMainHarness();
  assert.equal(typeof main.windowOpenHandler, 'function');
  for (const url of [
    'http://example.com',
    'https://example.com',
    'mailto:hi@example.com',
  ]) {
    assert.deepEqual(main.windowOpenHandler({ url }), { action: 'deny' });
  }
  assert.equal(main.calls.shell.length, 3);
  for (const url of ['file:///C:/x', 'javascript:alert(1)', 'not a url']) {
    assert.deepEqual(main.windowOpenHandler({ url }), { action: 'deny' });
  }
  assert.equal(main.calls.shell.length, 3);
});

test('navigation guards hand outside links to the system browser', () => {
  // Pinned by the real-Electron probe: a plain <a href="https://..."> (no
  // target) used to be cancelled with only a console line, so clicking it did
  // nothing at all. window.open already went to the system browser.
  for (const name of ['will-navigate', 'will-redirect']) {
    const main = createMainHarness();
    let prevented = 0;
    const event = { preventDefault: () => prevented++ };
    main.wc.emit(name, event, 'app://gleam/index.html');
    assert.equal(prevented, 0);
    const outside = [
      'https://example.com/page',
      'http://example.com',
      'mailto:hi@example.com',
    ];
    outside.forEach((url, i) => {
      main.wc.emit(name, event, url);
      assert.equal(prevented, i + 1, `${name} must still cancel in-app load`);
    });
    assert.deepEqual(main.calls.shell, outside);
    for (const url of ['file:///C:/x', 'javascript:alert(1)', 'app://evil/']) {
      main.wc.emit(name, event, url);
    }
    assert.equal(prevented, outside.length + 3);
    assert.equal(main.calls.shell.length, outside.length);
  }
  const main = createMainHarness();
  let webviewPrevented = 0;
  main.wc.emit('will-attach-webview', {
    preventDefault: () => webviewPrevented++,
  });
  assert.equal(webviewPrevented, 1);
});

test('session permissions grant only app-frame audio and clipboard writes', () => {
  const main = createMainHarness();
  let granted;
  main.permissionRequest('media', (ok) => (granted = ok), {
    requestingUrl: APP_URL,
    mediaTypes: ['audio'],
  });
  assert.equal(granted, true);
  main.permissionRequest('media', (ok) => (granted = ok), {
    requestingUrl: APP_URL,
    mediaTypes: ['audio', 'video'],
  });
  assert.equal(granted, false);
  main.permissionRequest('clipboard-sanitized-write', (ok) => (granted = ok), {
    requestingUrl: 'https://evil.example/',
  });
  assert.equal(granted, false);
  assert.equal(
    main.permissionCheck('clipboard-sanitized-write', 'app://gleam'),
    true,
  );
  assert.equal(
    main.permissionCheck('clipboard-sanitized-write', 'https://evil.example'),
    false,
  );
  let downloadPrevented = 0;
  main.session.emit('will-download', {
    preventDefault: () => downloadPrevented++,
  });
  assert.equal(downloadPrevented, 1);
});

test('other guarded channels stay reachable and reject unknown actions', async () => {
  const main = createMainHarness();
  const info = main.invoke('desktop:info');
  assert.equal(info.appVersion, '0.0.39');
  assert.equal(info.sidecarVersion, '0.0.39');
  assert.deepEqual(main.invoke('desktop:globalShortcut', 'get'), {
    ok: true,
    accelerator: '',
  });
  assert.throws(() => main.invoke('desktop:edit', 'evil'), /bad edit action/);
  assert.throws(() => main.invoke('desktop:view', 'evil'), /bad view action/);
  assert.throws(
    () => main.invoke('desktop:window', 'evil'),
    /bad window action/,
  );
  assert.deepEqual(await main.invoke('desktop:showOpenDialog', {}), [
    'C:/tmp/a.txt',
  ]);
});

// The computer-use module is not wired yet (pinned below), but its handlers
// must already be honest. With no backend an action fails, and the dry-run
// backend flags every result. The server used to fall back to the dry run on
// its own and answer success for a click that never happened.
function createComputerUseHarness(env) {
  const handlers = new Map();
  loadTsModule(COMPUTER_USE_MAIN, {
    electron: {
      ipcMain: { handle: (channel, fn) => handlers.set(channel, fn) },
    },
    env: {
      CUA_BINARY: path.join(os.tmpdir(), 'no-such-cua-binary'),
      ...env,
    },
    reset: [COMPUTER_USE_SERVER],
  });
  return {
    invoke: (channel, args) => handlers.get(channel)({}, args),
    channels: () => [...handlers.keys()].sort(),
  };
}

test('computer-use: with no backend, actions fail instead of faking success', async () => {
  const cu = createComputerUseHarness({ GLEAM_CUA_MODE: '' });
  assert.deepEqual(cu.invoke('computer-use:health'), {
    available: false,
    mode: 'unavailable',
    mock: false,
  });
  for (const [channel, args] of [
    ['computer-use:screenshot', undefined],
    ['computer-use:click', { x: 10, y: 20 }],
    ['computer-use:type', { text: 'hello' }],
    ['computer-use:open_app', { app_name: 'notepad' }],
  ]) {
    const result = await cu.invoke(channel, args);
    assert.equal(result.success, false, channel);
    assert.equal(result.unavailable, true, channel);
    assert.match(result.error, /no computer-use backend/);
  }
});

test('computer-use: the dry-run backend is opt-in and flags every result', async () => {
  const cu = createComputerUseHarness({ GLEAM_CUA_MODE: 'mock' });
  assert.deepEqual(cu.invoke('computer-use:health'), {
    available: false,
    mode: 'mock',
    mock: true,
  });
  const shot = await cu.invoke('computer-use:screenshot');
  assert.equal(shot.success, true);
  assert.equal(shot.mock, true);
  assert.equal(typeof shot.data, 'string');
  assert.equal(
    (await cu.invoke('computer-use:click', { x: 1, y: 2 })).mock,
    true,
  );
});

test('computer-use: invalid arguments never reach a backend', async () => {
  const cu = createComputerUseHarness({ GLEAM_CUA_MODE: 'mock' });
  const bad = [
    ['computer-use:click', { x: Number.NaN, y: 2 }, /finite numbers/],
    ['computer-use:type', { text: 42 }, /text must be a string/],
    ['computer-use:open_app', { app_name: '  ' }, /non-empty string/],
  ];
  for (const [channel, args, pattern] of bad) {
    const result = await cu.invoke(channel, args);
    assert.equal(result.success, false, channel);
    assert.equal(result.mock, undefined, channel);
    assert.match(result.error, pattern);
  }
});

// Known gap, pinned so a future wiring change cannot silently pass. The
// computer-use module is compiled into dist/ but neither imported by the
// main entry nor exposed by the main preload, so its channels have no owner.
// Its handlers also skip guard(), so wiring them in as-is would let any frame
// drive the desktop.
test('computer-use:* channels are compiled but not wired into the main entry', () => {
  const entry = readMain('index.ts');
  const channels = literalChannels(
    fs.readFileSync(COMPUTER_USE_MAIN, 'utf8'),
  ).sort();
  assert.deepEqual(channels, [
    'computer-use:click',
    'computer-use:health',
    'computer-use:open_app',
    'computer-use:screenshot',
    'computer-use:type',
  ]);
  assert.equal(/from ['"]\.\/computer_use/.test(entry), false);
  assert.equal(/require\(['"]\.\/computer_use/.test(entry), false);
});

// Keep-or-remove was decided on 2026-11-25: computer-use:* stays. The pin
// above stays in place; it fails the moment the module is wired, which is the
// reminder to bring the channels into the contract in the very same change.
test.todo(
  'register computer-use:* in the main entry behind guard() and add them to the IPC contract',
);
