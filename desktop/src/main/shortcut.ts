import { app, globalShortcut, BrowserWindow } from 'electron';
import { readFileSync, writeFileSync } from 'node:fs';
import { join } from 'node:path';

export interface ShortcutResult {
  ok: boolean;
  accelerator: string;
  error?: string;
}

let target: BrowserWindow | null = null;
let current = '';

function storePath(): string {
  return join(app.getPath('userData'), 'global-shortcut.json');
}

function load(): string {
  try {
    const raw = JSON.parse(readFileSync(storePath(), 'utf8')) as {
      accelerator?: unknown;
    };
    return typeof raw.accelerator === 'string' ? raw.accelerator : '';
  } catch {
    return '';
  }
}

function persist(accelerator: string): void {
  try {
    writeFileSync(storePath(), JSON.stringify({ accelerator }));
  } catch {}
}

function reveal(): void {
  if (!target || target.isDestroyed()) return;
  if (target.isMinimized()) target.restore();
  target.show();
  target.focus();
}

function apply(accelerator: string): ShortcutResult {
  if (current) {
    globalShortcut.unregister(current);
    current = '';
  }
  const accel = accelerator.trim();
  if (!accel) return { ok: true, accelerator: '' };
  try {
    if (!globalShortcut.register(accel, reveal)) {
      return {
        ok: false,
        accelerator: '',
        error: '这个组合已经被别的程序占用了',
      };
    }
  } catch (err) {
    return {
      ok: false,
      accelerator: '',
      error: `这个组合 Electron 不认：${(err as Error).message}`,
    };
  }
  current = accel;
  return { ok: true, accelerator: accel };
}

export function installGlobalShortcut(win: BrowserWindow): void {
  target = win;
  const saved = load();
  if (!saved) return;
  const r = apply(saved);
  console.log(
    `[desktop] 全局快捷键 ${saved}：${r.ok ? '已注册' : '没注册上（' + r.error + '）'}`,
  );
}

export function getShortcut(): string {
  return current;
}

export function setShortcut(accelerator: string): ShortcutResult {
  const r = apply(accelerator || '');
  if (r.ok) persist(r.accelerator);
  return r;
}
