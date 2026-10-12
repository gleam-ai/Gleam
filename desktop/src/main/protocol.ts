import { createHash } from 'node:crypto';
import { net, protocol } from 'electron';

export const APP_SCHEME = 'app';
export const APP_HOST = 'gleam';
export const APP_ORIGIN = `${APP_SCHEME}://${APP_HOST}`;
const TOKEN_HEADER = 'X-Gleam-Token';

export function registerAppScheme(): void {
  protocol.registerSchemesAsPrivileged([
    {
      scheme: APP_SCHEME,
      privileges: {
        standard: true,
        secure: true,
        supportFetchAPI: true,
        stream: true,
        codeCache: true,
      },
    },
  ]);
}

export interface Upstream {
  addr: string;
  token: string;
}

const DROP_HEADERS = new Set([
  'origin',
  'referer',
  'cookie',
  'host',
  TOKEN_HEADER.toLowerCase(),
]);

export function installAppProtocol(getUpstream: () => Upstream | null): void {
  protocol.handle(APP_SCHEME, async (req) => {
    const url = new URL(req.url);
    if (url.host !== APP_HOST)
      return new Response('not found', { status: 404 });
    const origin = req.headers.get('origin');
    if (origin && origin !== APP_ORIGIN)
      return new Response('forbidden origin', { status: 403 });
    const up = getUpstream();
    if (!up) return new Response('backend not running', { status: 503 });

    const headers = new Headers();
    req.headers.forEach((value, key) => {
      const k = key.toLowerCase();
      if (DROP_HEADERS.has(k) || k.startsWith('sec-fetch-')) return;
      headers.set(key, value);
    });
    headers.set(TOKEN_HEADER, up.token);

    const upstream = new AbortController();
    const init: RequestInit & {
      bypassCustomProtocolHandlers?: boolean;
      duplex?: 'half';
    } = {
      method: req.method,
      headers,
      signal: upstream.signal,
      bypassCustomProtocolHandlers: true,
    };
    if (req.body && req.method !== 'GET' && req.method !== 'HEAD') {
      init.body = req.body;
      init.duplex = 'half';
    }
    const res = await net.fetch(
      `http://${up.addr}${url.pathname}${url.search}`,
      init,
    );

    const type = res.headers.get('content-type') || '';
    if (!type.startsWith('text/html')) return cancelable(res, upstream);
    const html = await res.text();
    const out = new Headers(res.headers);
    out.set('Content-Security-Policy', buildCSP(html));
    out.delete('content-length');
    return new Response(html, {
      status: res.status,
      statusText: res.statusText,
      headers: out,
    });
  });
}

function cancelable(res: Response, upstream: AbortController): Response {
  if (!res.body) return res;
  const reader = res.body.getReader();
  const body = new ReadableStream<Uint8Array>({
    async pull(ctrl) {
      try {
        const { done, value } = await reader.read();
        if (done) ctrl.close();
        else ctrl.enqueue(value);
      } catch (err) {
        ctrl.error(err);
      }
    },
    cancel(reason) {
      upstream.abort(reason);
      return reader.cancel(reason).catch(() => {});
    },
  });
  return new Response(body, {
    status: res.status,
    statusText: res.statusText,
    headers: res.headers,
  });
}

function inlineScriptHashes(html: string): string[] {
  const hashes: string[] = [];
  const re = /<script(?![^>]*\bsrc=)[^>]*>([\s\S]*?)<\/script>/gi;
  for (let m = re.exec(html); m; m = re.exec(html)) {
    hashes.push(
      `'sha256-${createHash('sha256').update(m[1], 'utf8').digest('base64')}'`,
    );
  }
  return hashes;
}

function buildCSP(html: string): string {
  return [
    "default-src 'self'",
    `script-src 'self' ${inlineScriptHashes(html).join(' ')}`.trim(),
    "style-src 'self' 'unsafe-inline' https://fonts.googleapis.com",
    "font-src 'self' https://fonts.gstatic.com data:",
    "img-src 'self' data: blob: https:",
    "connect-src 'self'",
    'frame-src http: https:',
    "object-src 'none'",
    "base-uri 'none'",
    "form-action 'none'",
    "frame-ancestors 'none'",
  ].join('; ');
}
