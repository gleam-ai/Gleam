'use strict';

// End-to-end check against a real `gleam serve` process.
//
// Usage: node scripts/smoke-engine.js <path-to-gleam-binary>
//
// This is the one test that proves the extension's protocol layer matches the
// engine rather than matching itself. It needs no model: `initialize`, `ping`
// and `tools/list` all work with an unconfigured backend.

const { spawn } = require('node:child_process');
const { JsonRpcClient } = require('../src/rpc.js');

const bin = process.argv[2];
if (!bin) {
  console.error('usage: node scripts/smoke-engine.js <path-to-gleam-binary>');
  process.exit(2);
}

const child = spawn(bin, ['serve'], {
  stdio: ['pipe', 'pipe', 'pipe'],
  cwd: process.cwd(),
  windowsHide: true,
});
child.stderr.setEncoding('utf8');
child.stderr.on('data', (c) => process.stderr.write(`[engine] ${c}`));

const rpc = new JsonRpcClient({
  sendRaw: (chunk) => child.stdin.write(chunk),
  timeoutMs: 20000,
  onNotification: (method, params) => {
    console.log(`  notification ${method}`);
    if (method === 'goal/completed') Object.assign(seen, params);
  },
});
const seen = {};
child.stdout.setEncoding('utf8');
child.stdout.on('data', (chunk) => rpc.feed(chunk));

async function main() {
  const info = await rpc.request('initialize', {});
  console.log(
    `initialize -> ${info.name} ${info.version}, ${info.tools.length} tools`,
  );
  const pong = await rpc.request('ping', {});
  if (!pong || !pong.pong) throw new Error('ping did not come back positive');
  const tools = await rpc.request('tools/list', {});
  console.log(`tools/list -> ${JSON.stringify(tools).slice(0, 90)}…`);

  let rpcErr = null;
  try {
    await rpc.request('goal/submit', { goal: '' });
  } catch (err) {
    rpcErr = err;
  }
  if (!rpcErr || rpcErr.code !== -32602)
    throw new Error(`empty goal should be -32602, got ${rpcErr}`);
  console.log('goal/submit with an empty goal -> -32602 as expected');

  const listed = await rpc.request('skills/list', {});
  console.log(`skills/list -> ${JSON.stringify(listed).slice(0, 60)}…`);
  await rpc.request('shutdown', {});
  console.log('smoke OK');
}

main()
  .then(() => {
    child.kill();
    process.exit(0);
  })
  .catch((err) => {
    console.error(`smoke FAILED: ${err.message}`);
    child.kill();
    process.exit(1);
  });
