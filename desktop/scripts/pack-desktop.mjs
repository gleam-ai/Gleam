import { execFileSync } from 'node:child_process';
import { existsSync, statSync, readdirSync, rmSync } from 'node:fs';
import { dirname, join, resolve } from 'node:path';
import { fileURLToPath } from 'node:url';
import os from 'node:os';

const here = dirname(fileURLToPath(import.meta.url));
const desktopDir = resolve(here, '..');
const out = join(os.tmpdir(), 'gleam-pack');
const args = process.argv.slice(2);
if (!args.length) {
  console.error(
    '用法：node scripts/pack-desktop.mjs <electron-builder 的参数，如 --win nsis>',
  );
  process.exit(2);
}

function sweep() {
  for (const name of ['win-unpacked', 'win-unpacked.tmp', 'linux-unpacked']) {
    const dir = join(desktopDir, 'dist', name);
    if (existsSync(dir)) {
      rmSync(dir, { recursive: true, force: true });
      console.log(`[pack-desktop] 清掉残留产物 dist/${name}`);
    }
  }
}
sweep();

console.log(`[pack-desktop] 输出目录：${out}（工作区之外，索引器扫不到）`);
execFileSync(
  'npx',
  ['electron-builder', ...args, `--config.directories.output=${out}`],
  {
    cwd: desktopDir,
    stdio: 'inherit',
    shell: process.platform === 'win32',
  },
);

const show = (dir) => {
  for (const e of readdirSync(dir, { withFileTypes: true })) {
    if (!e.isFile()) continue;
    const p = join(dir, e.name);
    if (!/\.(exe|dmg|AppImage|deb|blockmap|yml)$/i.test(e.name)) continue;
    console.log(
      `[pack-desktop] ${(statSync(p).size / 1048576).toFixed(1)} MB  ${p}`,
    );
  }
};
if (existsSync(out)) {
  console.log('[pack-desktop] 产物：');
  show(out);
}
