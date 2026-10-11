import { existsSync, rmSync, statSync, readdirSync } from 'node:fs';
import { dirname, join, resolve } from 'node:path';
import { fileURLToPath } from 'node:url';

const here = dirname(fileURLToPath(import.meta.url));
const desktopDir = resolve(here, '..');
const platform = process.argv[2] || process.platform;
const arch = process.argv[3] || process.arch;
const dry = process.argv.includes('--dry');
const root = join(desktopDir, '.runtimes', `${platform}-${arch}`);

const TRIM_DIRS = new Set(['docs', 'doc', 'man', 'html', 'include']);

const KEEP_NAMES = /^(LICENSE|LICENCE|NOTICE|COPYING|SECURITY|README)/i;

function sizeOf(dir) {
  if (!existsSync(dir)) return 0;
  let total = 0;
  for (const entry of readdirSync(dir, { withFileTypes: true })) {
    const p = join(dir, entry.name);
    if (entry.isDirectory()) total += sizeOf(p);
    else total += statSync(p).size;
  }
  return total;
}

function collect(dir, depth, found) {
  if (!existsSync(dir)) return;
  for (const entry of readdirSync(dir, { withFileTypes: true })) {
    const p = join(dir, entry.name);
    if (!entry.isDirectory()) continue;
    if (
      depth >= 1 &&
      TRIM_DIRS.has(entry.name) &&
      !KEEP_NAMES.test(entry.name)
    ) {
      found.push(p);
      continue;
    }
    collect(p, depth + 1, found);
  }
}

const mb = (n) => (n / 1048576).toFixed(1);

if (!existsSync(root)) {
  console.log(
    `[trim-runtimes] ${root} not present, skipping (run fetch:runtimes first)`,
  );
  process.exit(0);
}

const targets = [];
collect(join(root, 'node'), 0, targets);
collect(join(root, 'uv'), 0, targets);

const before = sizeOf(root);
let saved = 0;
for (const target of targets) {
  const size = sizeOf(target);
  console.log(
    `[trim-runtimes] ${target.slice(root.length + 1)} -${mb(size)} MB`,
  );
  if (!dry) rmSync(target, { recursive: true, force: true });
  saved += size;
}

const after = dry ? before - saved : sizeOf(root);
console.log(
  `  ${mb(before)} MB -> ${mb(after)} MB (saves ${mb(saved)} MB${dry ? ', not written' : ''})`,
);
