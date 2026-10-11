import { execFileSync } from 'node:child_process';
import { createHash } from 'node:crypto';
import {
  existsSync,
  mkdirSync,
  readFileSync,
  rmSync,
  writeFileSync,
  renameSync,
  readdirSync,
  statSync,
} from 'node:fs';
import { basename, dirname, join, resolve } from 'node:path';
import { fileURLToPath } from 'node:url';

const here = dirname(fileURLToPath(import.meta.url));
const desktopDir = resolve(here, '..');
const platform = process.argv[2] || process.platform;
const arch = process.argv[3] || process.arch;
if (
  !['win32', 'darwin', 'linux'].includes(platform) ||
  !['x64', 'arm64'].includes(arch)
) {
  console.error(
    `不支持的平台 ${platform}-${arch}（只支持 win32/darwin/linux × x64/arm64）`,
  );
  process.exit(2);
}

const cacheDir = join(desktopDir, '.cache', 'runtimes');
const outDir = join(desktopDir, '.runtimes', `${platform}-${arch}`);
mkdirSync(cacheDir, { recursive: true });
mkdirSync(outDir, { recursive: true });

function previous() {
  const p = join(outDir, 'VERSIONS.json');
  if (!existsSync(p)) return {};
  try {
    return JSON.parse(readFileSync(p, 'utf8'));
  } catch {
    return {};
  }
}
const prev = previous();

const sha256 = (buf) => createHash('sha256').update(buf).digest('hex');

async function get(url) {
  const res = await fetch(url, { redirect: 'follow' });
  if (!res.ok) throw new Error(`下载失败 ${res.status} ${url}`);
  return Buffer.from(await res.arrayBuffer());
}

async function cached(url, name) {
  const p = join(cacheDir, name);
  if (existsSync(p)) {
    console.log(`  用缓存 ${name}`);
    return readFileSync(p);
  }
  console.log(`  下载 ${url}`);
  const buf = await get(url);
  writeFileSync(p, buf);
  return buf;
}

function verify(buf, want, what) {
  const got = sha256(buf);
  if (want && got.toLowerCase() !== want.toLowerCase()) {
    throw new Error(`${what} 的 sha256 对不上：期望 ${want}，实得 ${got}`);
  }
  return got;
}

function extract(archive, dest) {
  rmSync(dest, { recursive: true, force: true });
  mkdirSync(dest, { recursive: true });
  if (platform === 'win32') {
    execFileSync(
      'powershell',
      [
        '-NoProfile',
        '-Command',
        `Expand-Archive -LiteralPath '${archive}' -DestinationPath '${dest}' -Force`,
      ],
      { stdio: 'inherit' },
    );
  } else {
    execFileSync('tar', ['-xzf', archive, '-C', dest], { stdio: 'inherit' });
  }
}

function stripSingleWrapper(dir) {
  const entries = readdirSync(dir);
  if (entries.length === 1 && statSync(join(dir, entries[0])).isDirectory()) {
    const inner = join(dir, entries[0]);
    for (const e of readdirSync(inner))
      renameSync(join(inner, e), join(dir, e));
    rmSync(inner, { recursive: true, force: true });
  }
}

async function resolveNodeVersion() {
  if (process.env.NODE_VERSION) return process.env.NODE_VERSION;
  const index = JSON.parse(
    (await get('https://nodejs.org/dist/index.json')).toString('utf8'),
  );
  const lts = index.find((v) => v.lts);
  if (!lts) throw new Error('官方 index 里没找到 LTS 版本');
  return lts.version;
}

async function fetchNode() {
  const version = await resolveNodeVersion();
  const dst = join(outDir, 'node');
  if (
    reusable(prev.node, version, [
      join(dst, 'node.exe'),
      join(dst, 'bin', 'node'),
    ])
  ) {
    console.log(`[node] ${version}（已就绪，复用）`);
    return prev.node;
  }
  console.log(`[node] ${version}`);
  const base = `https://nodejs.org/dist/${version}`;
  const asset =
    platform === 'win32'
      ? `node-${version}-win-${arch === 'x64' ? 'x64' : 'arm64'}.zip`
      : platform === 'darwin'
        ? `node-${version}-darwin-${arch === 'x64' ? 'x64' : 'arm64'}.tar.gz`
        : `node-${version}-linux-${arch === 'x64' ? 'x64' : 'arm64'}.tar.gz`;
  const sums = (
    await cached(`${base}/SHASUMS256.txt`, `node-${version}-SHASUMS256.txt`)
  ).toString('utf8');
  const line = sums.split('\n').find((l) => l.trim().endsWith(asset));
  if (!line) throw new Error(`官方校验清单里没有 ${asset}`);
  const want = line.trim().split(/\s+/)[0];
  const buf = await cached(`${base}/${asset}`, asset);
  const got = verify(buf, want, `node ${version}`);
  const tmp = join(cacheDir, `x-node-${version}`);
  extract(join(cacheDir, asset), tmp);
  stripSingleWrapper(tmp);
  rmSync(dst, { recursive: true, force: true });
  renameSync(tmp, dst);
  return { version, asset, sha256: got, source: `${base}/${asset}` };
}

function reusable(entry, version, markers) {
  return (
    !!entry && entry.version === version && markers.some((m) => existsSync(m))
  );
}

const UV_TRIPLE = {
  'win32-x64': 'x86_64-pc-windows-msvc',
  'win32-arm64': 'aarch64-pc-windows-msvc',
  'darwin-x64': 'x86_64-apple-darwin',
  'darwin-arm64': 'aarch64-apple-darwin',
  'linux-x64': 'x86_64-unknown-linux-gnu',
  'linux-arm64': 'aarch64-unknown-linux-gnu',
};

async function fetchUV() {
  const rel = JSON.parse(
    (
      await get('https://api.github.com/repos/astral-sh/uv/releases/latest')
    ).toString('utf8'),
  );
  const version =
    process.env.UV_VERSION || String(rel.tag_name || '').replace(/^v/, '');
  if (!version) throw new Error('拿不到 uv 的最新版本号');
  const dst = join(outDir, 'uv');
  if (reusable(prev.uv, version, [join(dst, 'uv.exe'), join(dst, 'uv')])) {
    console.log(`[uv] ${version}（已就绪，复用）`);
    return prev.uv;
  }
  console.log(`[uv] ${version}`);
  const triple = UV_TRIPLE[`${platform}-${arch}`];
  const base = `https://github.com/astral-sh/uv/releases/download/${version}`;
  const asset =
    platform === 'win32' ? `uv-${triple}.zip` : `uv-${triple}.tar.gz`;
  const wantRaw = (await cached(`${base}/${asset}.sha256`, `${asset}.sha256`))
    .toString('utf8')
    .trim();
  const want = wantRaw.split(/\s+/)[0];
  const buf = await cached(`${base}/${asset}`, asset);
  const got = verify(buf, want, `uv ${version}`);
  const tmp = join(cacheDir, `x-uv-${version}`);
  extract(join(cacheDir, asset), tmp);
  stripSingleWrapper(tmp);
  rmSync(dst, { recursive: true, force: true });
  renameSync(tmp, dst);
  return { version, asset, sha256: got, source: `${base}/${asset}` };
}

async function fetchLicenses() {
  const dir = join(outDir, 'LICENSES');
  mkdirSync(dir, { recursive: true });
  const files = [
    [
      'https://raw.githubusercontent.com/nodejs/node/main/LICENSE',
      'node-LICENSE',
    ],
    [
      'https://raw.githubusercontent.com/astral-sh/uv/main/LICENSE-APACHE',
      'uv-LICENSE-APACHE',
    ],
    [
      'https://raw.githubusercontent.com/astral-sh/uv/main/LICENSE-MIT',
      'uv-LICENSE-MIT',
    ],
  ];
  const done = [];
  for (const [url, name] of files) {
    try {
      writeFileSync(join(dir, name), await get(url));
      done.push(name);
    } catch (err) {
      writeFileSync(
        join(dir, 'MISSING.txt'),
        `以下许可证本次构建没取到，发版前需要补齐：\n${name} <- ${url}\n原因：${err.message}\n`,
      );
      console.warn(`  [warn] 许可证 ${name} 没取到：${err.message}`);
    }
  }
  return done;
}

const result = {
  platform,
  arch,
  generatedAt: new Date().toISOString(),
  node: await fetchNode(),
  uv: await fetchUV(),
  licenses: await fetchLicenses(),
};
writeFileSync(
  join(outDir, 'VERSIONS.json'),
  JSON.stringify(result, null, 2) + '\n',
);

console.log(`\n运行时已就绪：${outDir}`);
console.log(
  `  node ${result.node.version}（${basename(result.node.asset)}，sha256 ${result.node.sha256.slice(0, 12)}…）`,
);
console.log(
  `  uv   ${result.uv.version}（${basename(result.uv.asset)}，sha256 ${result.uv.sha256.slice(0, 12)}…）`,
);
console.log(
  `  许可证：${result.licenses.join('、') || '（一个都没取到，见 LICENSES/MISSING.txt）'}`,
);
