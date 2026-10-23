import { app } from 'electron';
import {
  appendFileSync,
  mkdirSync,
  renameSync,
  statSync,
  rmSync,
} from 'node:fs';
import { join } from 'node:path';

const MAX_BYTES = 5 * 1024 * 1024;
let path = '';
let bytes = 0;
let installed = false;

function rotate(): void {
  const old = path + '.old';
  try {
    rmSync(old, { force: true });
    renameSync(path, old);
  } catch {}
  bytes = 0;
}

function fmt(value: unknown): string {
  if (typeof value === 'string') return value;
  if (value instanceof Error) return value.stack || value.message;
  try {
    return JSON.stringify(value);
  } catch {
    return String(value);
  }
}

function append(line: string): void {
  if (!path) return;
  try {
    if (bytes + line.length > MAX_BYTES) rotate();
    appendFileSync(path, line);
    bytes += Buffer.byteLength(line);
  } catch {}
}

// Packaged apps have no visible stdout, so mirror console output to a file
// next to the app's other logs (Help > Open Log Folder, or just ship it).
// Call again after the app is ready in case the logs path was unavailable
// before ready; the console hooks themselves are installed only once.
export function initFileLogging(): void {
  if (!installed) {
    installed = true;
    const levels = ['log', 'info', 'warn', 'error'] as const;
    for (const level of levels) {
      const original = console[level].bind(console);
      console[level] = (...args: unknown[]) => {
        original(...args);
        append(
          `${new Date().toISOString()} ${level.toUpperCase()} ${args.map(fmt).join(' ')}\n`,
        );
      };
    }
  }
  if (path) return;
  try {
    const dir = app.getPath('logs');
    mkdirSync(dir, { recursive: true });
    path = join(dir, 'main.log');
    try {
      bytes = statSync(path).size;
    } catch {
      bytes = 0;
    }
    if (bytes > MAX_BYTES) rotate();
  } catch {
    path = '';
    return;
  }
  append(`\n===== ${new Date().toISOString()} app start =====\n`);
}
