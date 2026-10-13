import { spawn, type ChildProcessWithoutNullStreams } from 'node:child_process';
import { randomBytes } from 'node:crypto';
import { EventEmitter } from 'node:events';
import { createInterface } from 'node:readline';
import { statSync } from 'node:fs';
import { pathWithRuntimes } from './runtimes';

export const READY_PREFIX = 'GLEAM_READY ';
const TOKEN_HEADER = 'X-Gleam-Token';

export interface SidecarSpec {
  bin: string;
  args: string[];
  cwd: string;
  extraPath?: string[];
}

export interface SidecarReady {
  addr: string;
  pid: number;
  version: string;
}

export class Sidecar extends EventEmitter {
  readonly token = randomBytes(32).toString('base64url');
  ready: SidecarReady | null = null;
  private child: ChildProcessWithoutNullStreams | null = null;
  private exited: Promise<void> = Promise.resolve();
  private stopping = false;

  constructor(private readonly spec: SidecarSpec) {
    super();
  }

  get running(): boolean {
    return (
      this.child !== null &&
      this.child.exitCode === null &&
      this.child.signalCode === null
    );
  }

  get pid(): number | undefined {
    return this.child?.pid;
  }

  start(timeoutMs = 15_000): Promise<SidecarReady> {
    // Guard against corrupted sidecar binary (e.g. NSIS installer copied over it).
    // The real sidecar is ~16 MB; the NSIS installer is ~250 MB.
    try {
      const size = statSync(this.spec.bin).size;
      if (size > 50 * 1024 * 1024) {
        throw new Error(
          `sidecar binary looks wrong (${(size / 1048576).toFixed(0)} MB — expected ~16 MB). ` +
          `This usually means the NSIS installer was copied over the sidecar during an update. ` +
          `Reinstall Gleam to fix.`,
        );
      }
    } catch (err) {
      if ((err as NodeJS.ErrnoException).code === 'ENOENT') {
        throw new Error(`sidecar binary not found: ${this.spec.bin}`);
      }
      if ((err as Error).message.includes('sidecar binary looks wrong')) throw err;
      // stat failed for other reasons — let spawn fail naturally
    }

    const env: NodeJS.ProcessEnv = {
      ...process.env,
      GLEAM_WEBUI_TOKEN: this.token,
    };
    if (this.spec.extraPath?.length) {
      env.PATH = pathWithRuntimes(env.PATH, this.spec.extraPath);
    }
    delete env.ELECTRON_RUN_AS_NODE;
    const child = spawn(this.spec.bin, ['desktop-sidecar', ...this.spec.args], {
      cwd: this.spec.cwd,
      env,
      stdio: ['pipe', 'pipe', 'pipe'],
      windowsHide: true,
      detached: process.platform !== 'win32',
    });
    this.child = child;
    this.exited = new Promise((resolve) => child.once('exit', () => resolve()));

    child.stderr.setEncoding('utf8');
    child.stderr.on('data', (d: string) => {
      for (const line of d.split('\n'))
        if (line.trim()) console.log(`[sidecar] ${line}`);
    });
    child.stdin.on('error', () => {});

    return new Promise<SidecarReady>((resolve, reject) => {
      let settled = false;
      const fail = (err: Error) => {
        if (settled) return;
        settled = true;
        clearTimeout(timer);
        this.forceKill();
        reject(err);
      };
      const timer = setTimeout(
        () => fail(new Error(`sidecar not ready within ${timeoutMs} ms`)),
        timeoutMs,
      );

      child.once('error', (err) => fail(err));
      child.once('exit', (code, signal) => {
        if (!settled)
          fail(
            new Error(
              `sidecar exited before ready (code=${code}, signal=${signal})`,
            ),
          );
        else if (!this.stopping) this.emit('unexpected-exit', code, signal);
      });

      const rl = createInterface({ input: child.stdout });
      rl.on('line', (line) => {
        if (settled || !line.startsWith(READY_PREFIX)) return;
        let info: SidecarReady;
        try {
          info = JSON.parse(line.slice(READY_PREFIX.length));
        } catch {
          fail(new Error(`malformed ready line: ${line}`));
          return;
        }
        this.probe(info.addr)
          .then(() => {
            if (settled) return;
            settled = true;
            clearTimeout(timer);
            this.ready = info;
            resolve(info);
          })
          .catch((err) =>
            fail(err instanceof Error ? err : new Error(String(err))),
          );
      });
    });
  }

  private async probe(addr: string): Promise<void> {
    const res = await fetch(`http://${addr}/api/info`, {
      headers: { [TOKEN_HEADER]: this.token },
    });
    if (res.status !== 200 || res.headers.get('x-gleam-server') !== 'gleam') {
      throw new Error(`sidecar probe failed: HTTP ${res.status}`);
    }
    await res.arrayBuffer();
  }

  async stop(graceMs = 5_000): Promise<'graceful' | 'killed' | 'not-running'> {
    const child = this.child;
    if (!child || !this.running) return 'not-running';
    this.stopping = true;
    try {
      child.stdin.write('{"cmd":"shutdown"}\n');
      child.stdin.end();
    } catch {}
    const timedOut = await Promise.race([
      this.exited.then(() => false),
      new Promise<boolean>((r) => setTimeout(() => r(true), graceMs)),
    ]);
    if (!timedOut) return 'graceful';
    this.forceKill();
    await Promise.race([this.exited, new Promise((r) => setTimeout(r, 2_000))]);
    return 'killed';
  }

  private forceKill(): void {
    const child = this.child;
    if (!child || child.pid === undefined || !this.running) return;
    if (process.platform === 'win32') {
      spawn('taskkill', ['/PID', String(child.pid), '/T', '/F'], {
        windowsHide: true,
        stdio: 'ignore',
      });
      return;
    }
    try {
      process.kill(-child.pid, 'SIGKILL');
    } catch {
      child.kill('SIGKILL');
    }
  }
}
