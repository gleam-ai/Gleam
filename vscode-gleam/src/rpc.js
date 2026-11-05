'use strict';

// JSON-RPC 2.0 over newline-delimited JSON, with no dependency on VS Code.
// Kept free of host APIs so it stays testable with `node --test`.

const DEFAULT_TIMEOUT_MS = 30000;

// Retryable error codes (network transient/server busy): can safely retry.
// Non-retryable codes (validation/authentication): do not retry.
const RETRYABLE_ERROR_CODES = new Set([-32000, -32001, -32002, -32003]);

// A timeout does not prove that the engine did not execute a request. Only
// methods whose repeated execution is safe may be retried by default.
const IDEMPOTENT_METHODS = new Set([
  'initialize',
  'ping',
  'shutdown',
  'goal/status',
  'goal/list',
  'goal/cancel',
  'tools/list',
  'memory/search',
  'skills/list',
  'skills/get',
]);

class JsonRpcError extends Error {
  constructor(code, message, data) {
    super(message || 'json-rpc error');
    this.name = 'JsonRpcError';
    this.code = code;
    this.data = data;
  }

  isRetryable() {
    return RETRYABLE_ERROR_CODES.has(this.code);
  }
}

class JsonRpcClient {
  constructor(options = {}) {
    this.sendRaw = options.sendRaw || (() => {});
    this.timeoutMs = options.timeoutMs || DEFAULT_TIMEOUT_MS;
    this.onNotification = options.onNotification || (() => {});
    this.onRequest = options.onRequest || (async () => undefined);
    this.nextId = 1;
    this.pending = new Map();
    this.buffer = '';
    this.closed = false;
    this.closeReason = 'transport closed';
  }

  // Feeds a chunk of the server's stdout. Partial lines are kept until the
  // newline arrives; ndjson guarantees one message per line.
  feed(chunk) {
    this.buffer += chunk;
    let nl;
    while ((nl = this.buffer.indexOf('\n')) >= 0) {
      const line = this.buffer.slice(0, nl);
      this.buffer = this.buffer.slice(nl + 1);
      const trimmed = line.trim();
      if (trimmed) this.receiveLine(trimmed);
    }
  }

  receiveLine(line) {
    let msg;
    try {
      msg = JSON.parse(line);
    } catch {
      return; // Not our protocol (e.g. a stray log line on stdout).
    }
    const { id, method } = msg;
    if (id === null) {
      // Go replies with id:null for parse/invalid-request errors. It can never
      // match a numeric pending id, so surface it instead of letting the
      // original caller wait out its full timeout.
      this.onNotification('rpc/serverError', msg.error || msg);
      return;
    }
    if (typeof id !== 'undefined' && (msg.result !== undefined || msg.error)) {
      this.settle(id, msg.result, msg.error);
      return;
    }
    if (typeof id !== 'undefined' && method) {
      return this.answerIncoming(id, method, msg.params);
    }
    if (method) this.onNotification(method, msg.params);
    return undefined;
  }

  async answerIncoming(id, method, params) {
    try {
      const result = await this.onRequest(method, params);
      this.send({
        jsonrpc: '2.0',
        id,
        result: result === undefined ? null : result,
      });
    } catch (err) {
      this.send({
        jsonrpc: '2.0',
        id,
        error: {
          code: err && err.code ? err.code : -32000,
          message: (err && err.message) || 'handler failed',
        },
      });
    }
  }

  settle(id, result, error) {
    const entry = this.pending.get(id);
    if (!entry) return;
    this.pending.delete(id);
    clearTimeout(entry.timer);
    if (error)
      entry.reject(new JsonRpcError(error.code, error.message, error.data));
    else entry.resolve(result);
  }

  send(payload) {
    this.sendRaw(JSON.stringify(payload) + '\n');
  }

  notify(method, params) {
    if (params === undefined) this.send({ jsonrpc: '2.0', method });
    else this.send({ jsonrpc: '2.0', method, params });
  }

  request(method, params, timeoutMs) {
    if (this.closed) {
      return Promise.reject(new JsonRpcError(-32002, this.closeReason));
    }
    const id = this.nextId++;
    return new Promise((resolve, reject) => {
      const timer = setTimeout(() => {
        this.pending.delete(id);
        reject(new JsonRpcError(-32001, `request timed out: ${method}`));
      }, timeoutMs || this.timeoutMs);
      this.pending.set(id, { resolve, reject, timer });
      try {
        if (params === undefined) this.send({ jsonrpc: '2.0', id, method });
        else this.send({ jsonrpc: '2.0', id, method, params });
      } catch (err) {
        this.pending.delete(id);
        clearTimeout(timer);
        reject(err);
      }
    });
  }

  // Called when the transport dies: pending callers must not hang forever and
  // this client must never be reused for a new transport.
  failAllPending(reason) {
    if (!this.closed) this.closeReason = reason || 'transport closed';
    this.closed = true;
    for (const [id, entry] of this.pending) {
      clearTimeout(entry.timer);
      entry.reject(new JsonRpcError(-32002, reason || 'transport closed'));
      this.pending.delete(id);
    }
  }
}

// RPC 包装器：统一错误处理与重试策略
async function rpcCallWithErrorHandling(client, method, params, options = {}) {
  const {
    maxRetries = 3,
    baseDelayMs = 500,
    timeoutMs = client.timeoutMs || DEFAULT_TIMEOUT_MS,
  } = options;

  let lastError;
  for (let attempt = 0; attempt <= maxRetries; attempt++) {
    try {
      return await client.request(method, params, timeoutMs);
    } catch (err) {
      lastError = err;
      if (
        client.closed ||
        !IDEMPOTENT_METHODS.has(method) ||
        !(err instanceof JsonRpcError) ||
        !err.isRetryable() ||
        attempt === maxRetries
      ) {
        throw err;
      }
      // Exponential backoff with jitter
      const delay = Math.min(baseDelayMs * Math.pow(2, attempt), 5000);
      const jitter = Math.random() * 0.2 * delay;
      await new Promise((res) => setTimeout(res, delay + jitter));
    }
  }
  throw lastError;
}

// 简单 Toast 提示接口（宿主层注入）
function toastNotify(message, type = 'info') {
  // 由宿主提供全局变量 gleamToast 或 console.warn 兜底
  if (typeof window !== 'undefined' && window.gleamToast) {
    window.gleamToast.notify({ message, type });
  } else {
    console.warn(`[${type.toUpperCase()}] ${message}`);
  }
}

// 导出新增工具
rpcCallWithErrorHandling.toastNotify = toastNotify;

module.exports = {
  JsonRpcClient,
  JsonRpcError,
  DEFAULT_TIMEOUT_MS,
  rpcCallWithErrorHandling,
};
