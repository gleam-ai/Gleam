'use strict';

const test = require('node:test');
const assert = require('node:assert');
const { EventEmitter } = require('node:events');
const Module = require('node:module');

function createHarness(t) {
  const harness = {
    processes: [],
    commands: new Map(),
    information: [],
    quickPickCalls: [],
    requestWaiters: [],
    holdGoalSubmit: false,
    cancelResult: true,
    nextTaskID: 1,
  };

  class FakeProcess extends EventEmitter {
    constructor() {
      super();
      this.requests = [];
      this.killed = false;
      this.stdout = new EventEmitter();
      this.stdout.setEncoding = () => {};
      this.stderr = new EventEmitter();
      this.stderr.setEncoding = () => {};
      this.stdin = new EventEmitter();
      this.stdin.writable = true;
      this.stdin.write = (chunk) => this.receiveRequest(JSON.parse(chunk));
    }

    receiveRequest(message) {
      this.requests.push(message);
      for (const waiter of [...harness.requestWaiters]) {
        if (waiter.method === message.method) {
          harness.requestWaiters.splice(
            harness.requestWaiters.indexOf(waiter),
            1,
          );
          waiter.resolve(message);
        }
      }
      if (message.method === 'goal/submit' && harness.holdGoalSubmit) return;

      let result = null;
      if (message.method === 'initialize') result = { name: 'gleam' };
      if (message.method === 'ping') result = { pong: true };
      if (message.method === 'goal/submit') {
        result = {
          task_id: `task-${harness.nextTaskID++}`,
          status: 'running',
        };
      }
      if (message.method === 'goal/cancel') {
        result = {
          task_id: message.params.task_id,
          cancelled: harness.cancelResult,
        };
      }
      queueMicrotask(() =>
        this.stdout.emit(
          'data',
          `${JSON.stringify({ jsonrpc: '2.0', id: message.id, result })}\n`,
        ),
      );
    }

    kill() {
      this.killed = true;
      this.stdin.writable = false;
    }
  }

  class ChangeEmitter {
    constructor() {
      this.listeners = new Set();
      this.event = (listener) => {
        this.listeners.add(listener);
        return { dispose: () => this.listeners.delete(listener) };
      };
    }

    fire(value) {
      for (const listener of this.listeners) listener(value);
    }

    dispose() {
      this.listeners.clear();
    }
  }

  const vscode = {
    EventEmitter: ChangeEmitter,
    TreeItem: class {
      constructor(label, collapsibleState) {
        this.label = label;
        this.collapsibleState = collapsibleState;
      }
    },
    TreeItemCollapsibleState: { None: 0 },
    StatusBarAlignment: { Right: 1 },
    Uri: { parse: (value) => ({ value }) },
    env: { openExternal: async () => true },
    workspace: {
      workspaceFolders: [],
      getConfiguration: () => ({
        get: (key, fallback) => (key === 'requestTimeoutMs' ? 100 : fallback),
      }),
      asRelativePath: (uri) => uri.fsPath,
    },
    commands: {
      registerCommand: (name, fn) => {
        harness.commands.set(name, fn);
        return { dispose: () => harness.commands.delete(name) };
      },
      executeCommand: async () => undefined,
    },
    window: {
      activeTextEditor: undefined,
      createOutputChannel: () => ({
        appendLine: () => {},
        show: () => {},
        dispose: () => {},
      }),
      createStatusBarItem: () => ({
        show: () => {},
        dispose: () => {},
      }),
      createTreeView: (_id, options) => {
        harness.treeProvider = options.treeDataProvider;
        return { dispose: () => {} };
      },
      showInformationMessage: async (message) => {
        harness.information.push(message);
      },
      showWarningMessage: async () => undefined,
      showInputBox: async () => 'test goal',
      showQuickPick: async (picks) => {
        harness.quickPickCalls.push(picks);
        return picks[0];
      },
    },
  };

  const extensionPath = require.resolve('../src/extension.js');
  delete require.cache[extensionPath];
  const originalLoad = Module._load;
  Module._load = function (request) {
    if (request === 'vscode') return vscode;
    if (request === 'node:child_process') {
      return {
        spawn: () => {
          const proc = new FakeProcess();
          harness.processes.push(proc);
          return proc;
        },
      };
    }
    return originalLoad.apply(this, arguments);
  };
  try {
    harness.extension = require(extensionPath);
  } finally {
    Module._load = originalLoad;
  }
  harness.extension.activate({ subscriptions: [] });
  t.after(() => harness.extension.deactivate());

  harness.start = async () => {
    await harness.commands.get('gleam.start')();
    return harness.processes[harness.processes.length - 1];
  };
  harness.submit = () => harness.commands.get('gleam.submitGoal')();
  harness.waitForRequest = (method) => {
    for (const proc of harness.processes) {
      const found = proc.requests.find((request) => request.method === method);
      if (found) return Promise.resolve(found);
    }
    return new Promise((resolve) =>
      harness.requestWaiters.push({ method, resolve }),
    );
  };
  return harness;
}

test('the task provider refreshes through onDidChangeTreeData', async (t) => {
  const harness = createHarness(t);
  assert.equal(typeof harness.treeProvider.onDidChangeTreeData, 'function');
  let changes = 0;
  const disposable = harness.treeProvider.onDidChangeTreeData(() => changes++);
  await harness.start();
  await harness.submit();
  disposable.dispose();
  assert.ok(changes > 0);
  assert.equal(harness.treeProvider.getChildren()[0].status, 'running');
});

test('stdin EPIPE rejects the pending call and the next start uses a new client', async (t) => {
  const harness = createHarness(t);
  harness.holdGoalSubmit = true;
  const proc = await harness.start();
  const submitting = harness.submit();
  await harness.waitForRequest('goal/submit');

  proc.stdin.writable = false;
  proc.stdin.emit(
    'error',
    Object.assign(new Error('write EPIPE'), { code: 'EPIPE' }),
  );
  await assert.rejects(submitting, (err) => {
    assert.equal(err.code, -32002);
    assert.match(err.message, /EPIPE/);
    return true;
  });
  assert.equal(
    proc.requests.filter((request) => request.method === 'goal/submit').length,
    1,
  );

  await harness.start();
  assert.equal(harness.processes.length, 2);
});

test('engine exit fails only its generation and cancellation follows the response', async (t) => {
  const harness = createHarness(t);
  const oldProcess = await harness.start();
  await harness.submit();
  oldProcess.emit('exit', 1, null);

  const oldTask = () =>
    harness.treeProvider.getChildren().find((entry) => entry.id === 'task-1');
  assert.equal(oldTask().status, 'failed');
  assert.match(oldTask().detail, /engine exited/);

  oldProcess.stdout.emit(
    'data',
    `${JSON.stringify({
      jsonrpc: '2.0',
      method: 'goal/progress',
      params: { task_id: 'task-1', title: 'stale progress' },
    })}\n`,
  );
  assert.equal(oldTask().status, 'failed');

  await harness.start();
  await harness.commands.get('gleam.cancelGoal')();
  assert.equal(harness.quickPickCalls.length, 0);
  assert.ok(harness.information.includes('No running task.'));

  await harness.submit();
  harness.cancelResult = false;
  await harness.commands.get('gleam.cancelGoal')();
  const currentTask = () =>
    harness.treeProvider.getChildren().find((entry) => entry.id === 'task-2');
  assert.deepEqual(
    harness.quickPickCalls[0].map((pick) => pick.id),
    ['task-2'],
  );
  assert.equal(currentTask().status, 'running');
  assert.equal(currentTask().detail, 'cancel was not accepted');

  harness.cancelResult = true;
  await harness.commands.get('gleam.cancelGoal')();
  assert.equal(currentTask().status, 'failed');
  assert.equal(currentTask().detail, 'cancelled');
});
