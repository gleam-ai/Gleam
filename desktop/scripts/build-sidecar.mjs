import { execFileSync } from 'node:child_process';
import { mkdirSync, readFileSync } from 'node:fs';
import { dirname, join, resolve } from 'node:path';
import { fileURLToPath } from 'node:url';

const here = dirname(fileURLToPath(import.meta.url));
const desktopDir = resolve(here, '..');
const repoRoot = resolve(desktopDir, '..');

const platform = process.argv[2] || process.platform;
const arch = process.argv[3] || process.arch;
const goos = { win32: 'windows', darwin: 'darwin', linux: 'linux' }[platform];
const goarch = { x64: 'amd64', arm64: 'arm64' }[arch];
if (!goos || !goarch) {
  console.error(`unsupported target ${platform}-${arch}`);
  process.exit(2);
}

const pkg = JSON.parse(readFileSync(join(desktopDir, 'package.json'), 'utf8'));
const version = pkg.version || '0.0.0';

const outDir = join(desktopDir, '.sidecar', `${platform}-${arch}`);
mkdirSync(outDir, { recursive: true });
const out = join(outDir, goos === 'windows' ? 'gleam.exe' : 'gleam');

const ldflags = `-s -w -X gleam/internal/buildinfo.Version=${version}`;
const args = [
  'build',
  '-trimpath',
  '-ldflags',
  ldflags,
  '-o',
  out,
  './cmd/gleam',
];
console.log(
  `[build-sidecar] GOOS=${goos} GOARCH=${goarch} go ${args.join(' ')}`,
);
execFileSync('go', args, {
  cwd: repoRoot,
  stdio: 'inherit',
  env: { ...process.env, GOOS: goos, GOARCH: goarch, CGO_ENABLED: '0' },
});
console.log(`[build-sidecar] -> ${out} (v${version})`);
