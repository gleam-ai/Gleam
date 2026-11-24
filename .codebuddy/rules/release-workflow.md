---
description: Gleam 发布流程规范——PR-only 工作流、签名推送、闸门验证、版本号同步。在执行任何发布相关操作（升版、打包、推送、建 PR、合并）前必须先读。
alwaysApply: false
enabled: true
paths: **/*.{go,ts,js,md,sh,py,yaml,yml}
---

# Gleam 发布流程规范（强制）

## 核心原则

1. **PR-only 工作流**：永远不许直接 push master。所有改动必须走分支 → PR → 合并。
2. **签名推送**：所有 commit 必须 GPG 签名（`git config --global commit.gpgsign true`）。
3. **闸门验证**：合并前必须通过 `scripts/verify.sh --quick`（至少到 layer 4）。
4. **版本号单一 owner**：版本号只从 `internal/buildinfo/buildinfo.go` 取，其他地方（前端、官网、package.json）必须同步但不许多抄。

## 升版流程（按顺序执行）

### 1. 本地改码 + 闸门

```bash
# 改版本号（三处）
# - internal/buildinfo/buildinfo.go: var Version = "x.y.z"
# - desktop/package.json: "version": "x.y.z"
# - desktop/package-lock.json: 两处 "version": "x.y.z"

# 同步官网徽标（check-version-owner.py 会查）
# - website/index.html: <span id="dl-version">vx.y.z</span>
# - website/index-zh.html: <span id="dl-version">vx.y.z</span>

# 跑闸门
gofmt -l ./internal ./pkg ./cmd
go vet ./...
go build ./...
go test ./... -count=1 -timeout 300s
bash scripts/verify.sh --quick
python scripts/check-version-owner.py
```

### 2. 本地提交（不签名也可以，后面会重签）

```bash
git checkout -b chore/release-x.y.z
git add -A
git commit -m "chore(release): bump to x.y.z"
```

### 3. 签名推送 + 建 PR

```bash
# 环境检查（失败则停）
gpg --list-secret-keys --keyid-format=long | grep <YOUR_KEY_ID>
ssh -T git@github.com 2>&1 | grep -q "successfully authenticated"

# 推送（需要代理）
export HTTPS_PROXY=http://127.0.0.1:6789 HTTP_PROXY=http://127.0.0.1:6789
git push -u origin chore/release-x.y.z

# 建 PR
gh pr create --title "chore(release): bump to x.y.z" --body "..."
```

### 4. 等 CI + CodeQL

```bash
# 等所有 checks 通过（包括 CodeQL）
gh pr checks <PR_NUMBER> --watch

# 如果 gh pr checks 报 GraphQL EOF，用 REST 轮询：
SHA=$(gh pr view <PR_NUMBER> --json headRefOid -q .headRefOid)
gh api repos/gleam-ai/Gleam/commits/$SHA/check-runs --jq '.check_runs[] | {name, status, conclusion}'
```

### 5. 合并（需要用户批准）

```bash
# 用户批准后执行
gh pr merge <PR_NUMBER> --admin --squash --delete-branch
```

### 6. 打包 + 装机实测

```bash
# CLI 全平台
bash scripts/release-all.sh

# Windows 安装包（需要代理）
export HTTPS_PROXY=http://127.0.0.1:6789 HTTP_PROXY=http://127.0.0.1:6789
cd desktop && npm run pack:win

# 装机实测（静默安装）
export MSYS_NO_PATHCONV=1 MSYS2_ARG_CONV_EXCL='*'
./dist/Gleam-Setup.exe /S '/D=C:\Software\Gleam'

# 版本校验（注意：用 resources/bin/gleam.exe，不是根目录的 Gleam.exe）
C:\Software\Gleam\resources\bin\gleam.exe version
# 应输出：gleam version x.y.z (built 2026-...)

# 硬门槛
C:\Software\Gleam\resources\bin\gleam.exe doctor --strict
C:\Software\Gleam\resources\bin\gleam.exe eval --layer smoke --strict --mock-llm
```

### 7. 发布（需要用户批准）

```bash
# 打标签
git tag vx.y.z
export HTTPS_PROXY=http://127.0.0.1:6789 HTTP_PROXY=http://127.0.0.1:6789
git push origin vx.y.z

# 建 Release + 上传 13 个资产
gh release create vx.y.z dist/* --title "v0.0.x" --notes "..."

# 验证 latest.yml
curl -s https://github.com/gleam-ai/Gleam/releases/latest/download/latest.yml | grep version
```

### 8. 同步官网发布日期（发布后 PR）

```bash
# 改 website/index.html 和 website/index-zh.html 的 dl-date
# <time id="dl-date" datetime="2026-xx-xx">2026-xx-xx</time>
```

## 常见陷阱

| 陷阱 | 症状 | 解决 |
|---|---|---|
| electron-builder 代理 | `pack:win` 报 ETIMEDOUT | 前置 `HTTPS_PROXY=... HTTP_PROXY=...` |
| cmd //c 交互陷阱 | 安装器不跑，停在 cmd 提示符 | 直接 exec：`./dist/Gleam-Setup.exe /S '/D=...'` |
| Gleam.exe 大小写冲突 | `gleam.exe version` 卡住 | 用 `resources/bin/gleam.exe` |
| 版本号漂移 | check-version-owner.py 报红 | 三处 + 官网两处全同步 |
| CodeQL 单独 gate | PR checks 显示 success 但 CodeQL 还在跑 | `gh pr checks --watch` 等所有 |

## 分工边界

- **codebuddy（本机）**：改码 + 闸门 + 本地提交
- **Qoder（或手动）**：签名推送 + 建 PR + 合并 + 打包 + 发布

如果 codebuddy 环境配好了（GPG/代理/SSH），可以执行全流程，但合并和发布前仍需用户批准。
