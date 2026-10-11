'use strict';

const test = require('node:test');
const assert = require('node:assert');
const { JsonRpcClient, JsonRpcError } = require('../src/rpc.js');

function client() {
  const out = [];
  return {
    out,
    rpc: new JsonRpcClient({ sendRaw: (chunk) => out.push(chunk) }),
  };
}

test('a request correlates with the reply that carries the same id', async () => {
  const { out, rpc } = client();
  const promise = rpc.request('tools/list');
  const sent = JSON.parse(out[0]);
  assert.equal(sent.id, 1);
  assert.equal(sent.method, 'tools/list');

  rpc.receiveLine(
    JSON.stringify({ jsonrpc: '2.0', id: 1, result: ['shell.run'] }),
  );
  assert.deepEqual(await promise, ['shell.run']);
});

test('null and falsy results resolve instead of being read as errors', async () => {
  const { out, rpc } = client();
  const promise = rpc.request('ping');
  rpc.receiveLine(
    JSON.stringify({ jsonrpc: '2.0', id: JSON.parse(out[0]).id, result: null }),
  );
  assert.equal(await promise, null);
});

test('an error reply rejects with its code and message', async () => {
  const { rpc } = client();
  const promise = rpc.request('goal/submit', { goal: '' }).catch((e) => e);
  rpc.receiveLine(
    JSON.stringify({
      jsonrpc: '2.0',
      id: 1,
      error: { code: -32602, message: 'goal 不能为空' },
    }),
  );
  const err = await promise;
  assert.ok(err instanceof JsonRpcError);
  assert.equal(err.code, -32602);
  assert.match(err.message, /goal/);
});

test('a message split across two chunks is still delivered whole', () => {
  const seen = [];
  const rpc = new JsonRpcClient({
    sendRaw: () => {},
    onNotification: (method, params) => seen.push([method, params]),
  });
  rpc.feed('{"jsonrpc":"2.0","method":"goal/progress","params":{"pct":4');
  rpc.feed('0}}\n');
  assert.deepEqual(seen, [['goal/progress', { pct: 40 }]]);
});

test('a non-JSON stdout line is ignored rather than thrown', () => {
  const { rpc } = client();
  assert.doesNotThrow(() => rpc.feed('listening on 127.0.0.1:8787\n'));
});

test('server-to-client requests are answered through the handler', async () => {
  const sent = [];
  const rpc = new JsonRpcClient({
    sendRaw: (chunk) => sent.push(chunk),
    onRequest: async (method) => ({ approved: method === 'goal/ask_approval' }),
  });
  await rpc.receiveLine(
    '{"jsonrpc":"2.0","id":7,"method":"goal/ask_approval","params":{"tool":"rm"}}',
  );
  const reply = JSON.parse(sent[0]);
  assert.equal(reply.id, 7);
  assert.deepEqual(reply.result, { approved: true });
});

test('a failing handler answers with an error, never hangs the peer', async () => {
  const sent = [];
  const rpc = new JsonRpcClient({
    sendRaw: (chunk) => sent.push(chunk),
    onRequest: async () => {
      throw new Error('user rejected');
    },
  });
  await rpc.receiveLine(
    '{"jsonrpc":"2.0","id":9,"method":"goal/ask_approval","params":{}}',
  );
  const reply = JSON.parse(sent[0]);
  assert.equal(reply.id, 9);
  assert.equal(reply.error.code, -32000);
  assert.equal(reply.error.message, 'user rejected');
});

test('closing the transport rejects every pending caller', async () => {
  const { out, rpc } = client();
  const first = rpc.request('goal/status').catch((e) => e);
  const second = rpc.request('tools/list').catch((e) => e);
  assert.equal(out.length, 2);
  rpc.failAllPending('engine exited');
  for (const p of [first, second]) {
    const err = await p;
    assert.equal(err.code, -32002);
  }
});

test('a request past its deadline rejects and stops tracking the id', async () => {
  const { rpc } = client();
  const promise = rpc.request('goal/status', {}, 10).catch((e) => e);
  const err = await promise;
  assert.equal(err.code, -32001);
  assert.equal(rpc.pending.size, 0);
});
