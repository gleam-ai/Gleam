'use strict';

const { spawn } = require('node:child_process');
const vscode = require('vscode');
const { JsonRpcClient } = require('./rpc.js');

let client = null;
let child = null;
let starting = null;
let channel = null;
let statusItem = null;
let view = null;
const tasks = new Map();

function cfg(key, fallback) {
  return vscode.workspace.getConfiguration('gleam').get(key, fallback);
}

function workspaceRoot() {
  const explicit = cfg('workingDirectory', '');
  if (explicit) return explicit;
  const folders = vscode.workspace.workspaceFolders;
  return folders && folders.length ? folders[0].uri.fsPath : process.cwd();
}

function log(line) {
  channel.appendLine(line);
}

function setStatus(text, tooltip) {
  statusItem.text = `$(sparkle) ${text}`;
  statusItem.tooltip = tooltip || 'Gleam engine';
  statusItem.show();
}

function taskLabel(entry) {
  const mark = { running: '$(sync)', succeeded: '$(check)', failed: '$(x)' }[
    entry.status
  ];
  return `${mark || '$(circle-outline)'} ${entry.goal}`;
}

const provider = {
  getTreeItem: (entry) => {
    const item = new vscode.TreeItem(
      taskLabel(entry),
      vscode.TreeItemCollapsibleState.None,
    );
    item.id = entry.id;
    item.description = entry.status;
    item.tooltip = entry.detail || entry.goal;
    item.command = {
      command: 'gleam.showResult',
      title: 'Show result',
      arguments: [entry.id],
    };
    return item;
  },
  getChildren: () => [...tasks.values()].reverse().slice(0, 25),
};

function record(id, goal, status, detail) {
  tasks.set(id, {
    id,
    goal,
    status: status || 'running',
    detail: detail || '',
  });
  if (view) view.refresh();
}

function noteTask(id, patch) {
  const entry = tasks.get(id);
  if (entry) Object.assign(entry, patch);
  if (view) view.refresh();
}

async function ensureClient() {
  if (client) return client;
  if (starting) return starting;
  starting = startClient();
  try {
    return await starting;
  } finally {
    starting = null;
  }
}

async function startClient() {
  const command = cfg('command', 'gleam');
  const args = cfg('args', ['serve']);
  channel = channel || vscode.window.createOutputChannel('Gleam');
  const proc = spawn(command, args, {
    cwd: workspaceRoot(),
    stdio: ['pipe', 'pipe', 'pipe'],
    windowsHide: true,
  });
  child = proc;
  const rpc = new JsonRpcClient({
    sendRaw: (chunk) => {
      if (proc.stdin.writable) proc.stdin.write(chunk);
    },
    timeoutMs: cfg('requestTimeoutMs', 30000),
    onNotification: (method, params) => {
      if (method === 'goal/progress') {
        const p = params || {};
        log(
          `[${p.phase || 'progress'}] ${p.title || p.detail || ''}`.trimEnd(),
        );
        if (p.task_id)
          noteTask(p.task_id, {
            status: 'running',
            detail: p.title || p.detail,
          });
      } else if (method === 'goal/tool') {
        const p = params || {};
        log(`  ${p.tool}: ${p.title || ''}${p.error ? ` (${p.error})` : ''}`);
      } else if (method === 'goal/completed') {
        const p = params || {};
        record(
          p.task_id,
          p.goal || '',
          p.status || 'succeeded',
          p.summary || p.error || '',
        );
        log(`done ${p.task_id}: ${p.summary || p.status}`);
        if (p.suggestion) log(`suggestion: ${p.suggestion}`);
        setStatus('idle');
      } else if (method === 'agent/suggestion') {
        vscode.window.showInformationMessage(
          `Gleam: ${(params || {}).text || ''}`,
        );
      } else {
        log(`${method} ${JSON.stringify(params || {})}`);
      }
    },
    onRequest: onEngineRequest,
  });
  client = rpc;
  const cleanup = () => {
    if (client === rpc) client = null;
    if (child === proc) child = null;
  };
  proc.stdout.setEncoding('utf8');
  // 绑定当前实例：旧进程的迟到输出不能喂进新 client。
  proc.stdout.on('data', (chunk) => rpc.feed(chunk));
  proc.stderr.setEncoding('utf8');
  proc.stderr.on('data', (chunk) => {
    for (const line of String(chunk).split('\n'))
      if (line.trim()) log(`stderr: ${line}`);
  });
  proc.on('error', (err) => {
    log(`engine spawn failed: ${err.message}`);
    setStatus('stopped', `engine failed to start: ${err.message}`);
    rpc.failAllPending(`engine failed to start: ${err.message}`);
    cleanup();
  });
  proc.on('exit', (code, signal) => {
    setStatus('stopped', `engine exited (code ${code}, signal ${signal})`);
    log(`engine exited: code=${code} signal=${signal}`);
    rpc.failAllPending('engine exited');
    cleanup();
  });
  try {
    await rpc.request('initialize', {});
    const info = await rpc.request('ping');
    log(`engine ready: ${JSON.stringify(info)}`);
    setStatus('ready', `${command} ${args.join(' ')}`);
    return rpc;
  } catch (err) {
    // 握手失败不能把坏 client 留在全局：否则后续每个命令都等满 30 秒。
    log(`engine handshake failed: ${err && err.message ? err.message : err}`);
    try {
      proc.kill();
    } catch {}
    rpc.failAllPending('engine handshake failed');
    cleanup();
    throw err;
  }
}

async function onEngineRequest(method, params) {
  if (method !== 'goal/ask_approval') {
    throw new Error(`unsupported request: ${method}`);
  }
  const p = params || {};
  const risk = p.risk || 'unknown';
  const lines = [p.reason || 'Approve this step?', `risk: ${risk}`];
  for (const step of p.plan || []) lines.push(`- ${step}`);
  const detail = { modal: true, detail: lines.join('\n') };
  const answer = await vscode.window.showWarningMessage(
    'Gleam needs approval',
    detail,
    'Approve',
    'Approve with note',
    'Reject',
  );
  if (!answer || answer === 'Reject') {
    return {
      approved: false,
      note: answer === 'Reject' ? 'rejected by user' : 'dismissed',
    };
  }
  let note = '';
  if (answer === 'Approve with note') {
    note =
      (await vscode.window.showInputBox({ prompt: 'Note for Gleam' })) || '';
  }
  return { approved: true, note };
}

async function submit(options) {
  const c = await ensureClient();
  const goal = options && options.goal;
  if (!goal) return;
  setStatus('running', goal);
  const res = await c.request('goal/submit', {
    goal,
    mode: (options && options.mode) || cfg('defaultMode', 'auto'),
    task_mode: (options && options.taskMode) || cfg('defaultTaskMode', 'auto'),
    references: (options && options.references) || [],
  });
  record(res.task_id, goal, 'running');
  log(`submitted ${res.task_id}: ${goal}`);
  vscode.window.showInformationMessage(
    `Gleam is working on it (task ${res.task_id}).`,
  );
}

async function goalFromSelection(prompt) {
  const editor = vscode.window.activeTextEditor;
  const added = [];
  if (editor) {
    const selected = editor.document.getText(editor.selection);
    if (selected.trim()) {
      added.push({
        kind: 'file',
        label: vscode.workspace.asRelativePath(editor.document.uri),
        value: selected,
      });
    }
  }
  const input = await vscode.window.showInputBox({
    prompt: prompt || 'What should Gleam do?',
    placeHolder: 'e.g. explain this function and its failure modes',
    value: '',
  });
  return input ? { goal: input, references: added } : null;
}

function register(context, name, fn) {
  context.subscriptions.push(vscode.commands.registerCommand(name, fn));
}

function activate(context) {
  channel = vscode.window.createOutputChannel('Gleam');
  statusItem = vscode.window.createStatusBarItem(
    vscode.StatusBarAlignment.Right,
    100,
  );
  context.subscriptions.push(channel, statusItem);
  setStatus('stopped', 'Gleam engine is not running yet');

  view = vscode.window.createTreeView('gleamGoals', {
    treeDataProvider: provider,
  });
  context.subscriptions.push(view);

  vscode.commands.executeCommand('setContext', 'gleam.enabled', true);

  register(context, 'gleam.start', async () => {
    await ensureClient();
    vscode.window.showInformationMessage('Gleam engine started.');
  });

  register(context, 'gleam.stop', async () => {
    if (!client) return;
    try {
      await client.request('shutdown', {});
    } catch {
      /* engine may already be gone; kill below is the real cleanup */
    }
    if (child) child.kill();
  });

  register(context, 'gleam.submitGoal', async () => {
    const spec = await goalFromSelection();
    if (spec) await submit(spec);
  });

  register(context, 'gleam.submitSelection', async () => {
    const editor = vscode.window.activeTextEditor;
    if (!editor) return;
    const selected = editor.document.getText(editor.selection);
    if (!selected.trim()) {
      vscode.window.showWarningMessage('Select some code first.');
      return;
    }
    await submit({
      goal: `Explain this code and point out anything fragile:\n\n${selected}`,
      references: [
        {
          kind: 'file',
          label: vscode.workspace.asRelativePath(editor.document.uri),
          value: selected,
        },
      ],
    });
  });

  register(context, 'gleam.cancelGoal', async () => {
    if (!client) return;
    const picks = [...tasks.values()].filter((t) => t.status === 'running');
    if (!picks.length) {
      vscode.window.showInformationMessage('No running task.');
      return;
    }
    const pick = await vscode.window.showQuickPick(
      picks.map((t) => ({ label: t.goal, id: t.id })),
      { placeHolder: 'Cancel which task?' },
    );
    if (!pick) return;
    await client.request('goal/cancel', { task_id: pick.id });
    noteTask(pick.id, { status: 'failed', detail: 'cancelled' });
  });

  register(context, 'gleam.showResult', async (taskID) => {
    const id =
      taskID ||
      (
        await vscode.window.showQuickPick(
          [...tasks.values()].map((t) => ({ label: t.goal, id: t.id })),
          {
            placeHolder: 'Which task result?',
          },
        )
      )?.id;
    if (!id) return;
    const status = await (
      await ensureClient()
    ).request('goal/status', { task_id: id });
    const doc = await vscode.workspace.openTextDocument({
      content: JSON.stringify(status, null, 2),
      language: 'json',
    });
    await vscode.window.showTextDocument(doc, { preview: true });
  });

  register(context, 'gleam.openUI', async () => {
    const url = cfg('uiUrl', 'http://127.0.0.1:8787');
    await vscode.env.openExternal(vscode.Uri.parse(url));
  });

  register(context, 'gleam.showOutput', () => channel.show(true));
}

function deactivate() {
  if (child) child.kill();
  child = null;
  client = null;
}

module.exports = { activate, deactivate };
