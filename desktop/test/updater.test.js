'use strict';

const test = require('node:test');
const assert = require('node:assert');
const { EventEmitter } = require('node:events');
const fs = require('node:fs');
const Module = require('node:module');
const ts = require('typescript');

function createHarness(hostOverrides = {}, { packaged = true } = {}) {
  const handlers = new Map();
  const app = new EventEmitter();
  app.isPackaged = packaged;
  app.quit = () => {};
  const autoUpdater = new EventEmitter();
  autoUpdater.installerPath = 'installer.exe';
  autoUpdater.setFeedURL = () => {};
  autoUpdater.checkForUpdates = async () => null;
  autoUpdater.downloadUpdate = async () => null;
  autoUpdater.quitAndInstall = () => {};

  const electron = {
    app,
    BrowserWindow: { getAllWindows: () => [] },
    ipcMain: {
      handle: (name, handler) => handlers.set(name, handler),
    },
    net: {},
    protocol: {},
  };
  const host = {
    allowClose: () => {},
    quitForUpdate: async () => false,
    resumeAfterFailedUpdate: async () => {},
    upstream: () => null,
    ...hostOverrides,
  };

  const updaterPath = require.resolve('../src/main/updater.ts');
  const protocolPath = require.resolve('../src/main/protocol.ts');
  delete require.cache[updaterPath];
  delete require.cache[protocolPath];
  const originalLoad = Module._load;
  const originalTypeScriptLoader = require.extensions['.ts'];
  require.extensions['.ts'] = (module, filename) => {
    const output = ts.transpileModule(fs.readFileSync(filename, 'utf8'), {
      compilerOptions: {
        module: ts.ModuleKind.CommonJS,
        target: ts.ScriptTarget.ES2022,
        esModuleInterop: true,
      },
    }).outputText;
    module._compile(output, filename);
  };
  const previousAppImage = process.env.APPIMAGE;
  process.env.APPIMAGE = 'test.AppImage';
  Module._load = function (request) {
    if (request === 'electron') return electron;
    if (request === 'electron-updater') return { autoUpdater };
    if (request === 'node:child_process') {
      return { spawn: () => assert.fail('installer must not be spawned') };
    }
    return originalLoad.apply(this, arguments);
  };
  try {
    require(updaterPath).installUpdater(host);
  } finally {
    Module._load = originalLoad;
    if (originalTypeScriptLoader) {
      require.extensions['.ts'] = originalTypeScriptLoader;
    } else {
      delete require.extensions['.ts'];
    }
    if (previousAppImage === undefined) delete process.env.APPIMAGE;
    else process.env.APPIMAGE = previousAppImage;
  }

  const event = { senderFrame: { parent: null, url: 'app://gleam/' } };
  return {
    autoUpdater,
    invoke: (name) => handlers.get(name)(event),
  };
}

test('development mode exposes updater IPC without checking', async () => {
  const harness = createHarness({}, { packaged: false });
  let checks = 0;
  harness.autoUpdater.checkForUpdates = async () => {
    checks++;
  };

  assert.deepEqual(await harness.invoke('desktop:update:status'), {
    kind: 'idle',
  });
  assert.deepEqual(await harness.invoke('desktop:update:check'), { ok: false });
  assert.equal(checks, 0);
});

test('concurrent restart requests share one sidecar shutdown', async () => {
  let quitCalls = 0;
  let finishQuit;
  const harness = createHarness({
    quitForUpdate: () => {
      quitCalls++;
      return new Promise((resolve) => {
        finishQuit = resolve;
      });
    },
  });
  harness.autoUpdater.emit('update-available', { version: '1.2.3' });
  harness.autoUpdater.emit('update-downloaded', { version: '1.2.3' });

  const first = harness.invoke('desktop:update:restart');
  const second = await harness.invoke('desktop:update:restart');
  assert.deepEqual(second, {
    ok: false,
    error: 'restart already in progress',
  });
  assert.equal(quitCalls, 1);

  finishQuit(false);
  assert.deepEqual(await first, {
    ok: false,
    error: 'backend still running',
  });
});

test('update errors keep the status and engine reason, not the raw request', async () => {
  const harness = createHarness();
  const err = Object.assign(
    new Error('502 "Bad Gateway"\nHeaders: {"x-gleam-token":"secret"}'),
    {
      statusCode: 502,
      statusText: 'Bad Gateway',
      description: JSON.stringify({
        error: '连不上 GitHub 更新源：dial tcp 1.2.3.4:443: connectex failed',
      }),
    },
  );
  harness.autoUpdater.emit('error', err);
  const status = await harness.invoke('desktop:update:status');
  assert.equal(status.kind, 'error');
  assert.equal(
    status.message,
    '502 Bad Gateway · 连不上 GitHub 更新源：dial tcp 1.2.3.4:443: connectex failed',
  );
  assert.equal(/x-gleam-token|Headers/.test(status.message), false);
});

test('an HTML error body never reaches the dialog', async () => {
  const harness = createHarness();
  harness.autoUpdater.emit(
    'error',
    Object.assign(new Error('404 Not Found'), {
      statusCode: 404,
      description: '<html><body>Not Found</body></html>',
    }),
  );
  const status = await harness.invoke('desktop:update:status');
  assert.equal(status.message, '404');
});

test('a failed download returns to available and can be retried', async () => {
  const harness = createHarness();
  harness.autoUpdater.emit('update-available', { version: '1.2.3' });
  let attempts = 0;
  harness.autoUpdater.downloadUpdate = async () => {
    attempts++;
    harness.autoUpdater.emit('error', new Error('offline'));
    throw new Error('offline');
  };

  const failed = await harness.invoke('desktop:update:download');
  assert.equal(failed.ok, false);
  assert.deepEqual(await harness.invoke('desktop:update:status'), {
    kind: 'available',
    version: '1.2.3',
  });

  harness.autoUpdater.downloadUpdate = async () => {
    attempts++;
    return null;
  };
  assert.deepEqual(await harness.invoke('desktop:update:download'), {
    ok: true,
  });
  assert.equal(attempts, 2);
});
