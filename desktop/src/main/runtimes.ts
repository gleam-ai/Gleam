import { execFile } from 'node:child_process';
import { existsSync } from 'node:fs';
import { join } from 'node:path';
import type { Lang } from './locale';

export interface RuntimeProbe {
  name: 'node' | 'uv' | 'git';
  label: string;
  ok: boolean;
  bundled: boolean;
  version: string;
  detail: string;
}

export function runtimesRoot(): string {
  const packaged = join(process.resourcesPath, 'runtimes');
  if (existsSync(packaged)) return packaged;
  return join(
    __dirname,
    '..',
    '..',
    '.runtimes',
    `${process.platform}-${process.arch}`,
  );
}

export function runtimeBinDirs(): string[] {
  const root = runtimesRoot();
  const nodeDir = join(root, 'node');
  // Unix keeps node/npx under node/bin; Windows puts them directly under node/.
  const candidates =
    process.platform === 'win32'
      ? [nodeDir, join(root, 'uv')]
      : [join(nodeDir, 'bin'), nodeDir, join(root, 'uv')];
  return candidates.filter((d) => existsSync(d));
}

export function pathWithRuntimes(
  pathValue: string | undefined,
  dirs = runtimeBinDirs(),
): string {
  if (dirs.length === 0) return pathValue ?? '';
  return (
    dirs.join(process.platform === 'win32' ? ';' : ':') +
    (pathValue ? (process.platform === 'win32' ? ';' : ':') + pathValue : '')
  );
}

function versionOf(command: string, args: string[]): Promise<string> {
  return new Promise((resolve) => {
    execFile(
      command,
      args,
      { timeout: 8000, windowsHide: true },
      (err, stdout, stderr) => {
        if (err) return resolve('');
        const first =
          String(stdout || stderr || '')
            .trim()
            .split(/\r?\n/)[0] ?? '';
        resolve(first.replace(/^v/, '').trim());
      },
    );
  });
}

export async function probeRuntimes(
  lang: Lang = 'zh',
): Promise<RuntimeProbe[]> {
  const root = runtimesRoot();
  const isWin = process.platform === 'win32';
  const nodeExe = join(root, 'node', isWin ? 'node.exe' : 'bin/node');
  const uvExe = join(root, 'uv', isWin ? 'uv.exe' : 'uv');
  const npxName = isWin ? 'npx.cmd' : 'npx';
  const uvxName = isWin ? 'uvx.exe' : 'uvx';

  const nodeVer = existsSync(nodeExe) ? await versionOf(nodeExe, ['-v']) : '';
  const npxThere = existsSync(join(root, 'node', isWin ? '' : 'bin', npxName));
  const uvVer = existsSync(uvExe) ? await versionOf(uvExe, ['--version']) : '';
  const uvxThere = existsSync(join(root, 'uv', uvxName));
  const gitVer = await versionOf('git', ['--version']);

  const zh = lang === 'zh';
  return [
    {
      name: 'node',
      label: zh ? 'Node.js（内置，含 npx）' : 'Node.js (bundled, includes npx)',
      ok: !!nodeVer && npxThere,
      bundled: true,
      version: nodeVer,
      detail:
        nodeVer && npxThere
          ? zh
            ? '市场里用 npx 起的 MCP 服务器可以直接装'
            : 'MCP servers that run through npx install straight from the market'
          : zh
            ? '内置的 Node 没跑起来（可能被安全软件拦了）。用到 npx 的功能会提示重装'
            : 'The bundled Node did not start (security software may have blocked it). Features that need npx will offer to reinstall',
    },
    {
      name: 'uv',
      label: zh ? 'uv（内置，含 uvx）' : 'uv (bundled, includes uvx)',
      ok: !!uvVer && uvxThere,
      bundled: true,
      version: uvVer,
      detail:
        uvVer && uvxThere
          ? zh
            ? '市场里用 uvx 起的 Python 类 MCP 服务器可以直接装'
            : 'Python-based MCP servers that run through uvx install straight from the market'
          : zh
            ? '内置的 uv 没跑起来。用到 uvx 的功能会提示重装'
            : 'The bundled uv did not start. Features that need uvx will offer to reinstall',
    },
    {
      name: 'git',
      label: zh ? 'Git（用你系统里的）' : 'Git (from your system)',
      ok: !!gitVer,
      bundled: false,
      version: gitVer.replace(/^git version\s*/i, ''),
      detail: gitVer
        ? zh
          ? '编程任务里的分支、提交、worktree 都用它'
          : 'Used for branches, commits and worktrees in coding tasks'
        : zh
          ? '没检测到 Git：编程类任务会提示先装。内置不打包它（体积几百 MB，系统里通常已经有了）'
          : 'No Git detected: coding tasks will ask you to install it. Not bundled (hundreds of MB, and most systems already have it)',
    },
  ];
}
