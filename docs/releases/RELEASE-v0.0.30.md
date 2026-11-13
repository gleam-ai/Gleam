# Gleam v0.0.30 — 撤回 v0.0.29 的权限放行

> v0.0.29 在做「完全访问不再弹审批」时放得太宽：`desktop.clipboard.read` 可能带出密码，却被一并放行。CI 的权限测试拦住了这个回归。本版撤回该改动，并把文案改成与实际一致。

**版本**: v0.0.30
**日期**: 2026-11-05
**基线**: v0.0.29

## ⚠️ 请勿使用 v0.0.29

v0.0.29 让「完全访问」跳过了无路径中风险工具的一次性确认，其中包括剪贴板读取（可能带出密码）。请升级到本版。

## 📦 下载

| 文件 | 说明 |
| --- | --- |
| `Gleam-Setup.exe` | Windows 桌面安装包（NSIS，含 Electron 45.2.1、内置 Node/uv 运行时与 gleam v0.0.30 sidecar）；`latest.yml` + blockmap 供自动更新 |
| `gleam-windows-amd64.exe` | Windows CLI（控制台版） |
| `gleam-darwin-amd64` / `gleam-darwin-arm64` | macOS（Intel / Apple Silicon） |
| `gleam-linux-amd64` | Linux x86_64 |
| `SHA256SUMS.txt` | 全部资产校验和 |

## 🩹 本版改动

### 1. 撤回「完全访问放行中风险无路径工具」

完全访问解除的是**路径限制**，不是敏感工具的一次性确认。现在：

- `desktop.clipboard.read`、`memory.save`、`prompt.run`、`web.fetch`、`agent.delegate` 首次仍需确认一次
- 批准后经 `TrustForSession` 记入会话信任白名单，本会话不再重复询问
- `desktop.clipboard.write` **始终**不在信任白名单内，每次都要审批
- 高风险工具在任何模式下都强制审批

### 2. 文案改为与实际一致

设置页、底部权限菜单与切换提示此前写「仅高风险操作需要批准」，与实现不符。现统一为：

- 完全访问：**路径不设限；高风险操作与敏感工具仍需确认一次**
- 自动审批：**路径不设限；敏感工具首次确认后，本会话不再询问**

### 3. 保留 v0.0.29 的两项独立修复

- **最终回复不再丢失**：模型把 `reply.text` 传成段落数组或包裹对象时，`ArgNormalizer` 在校验前展平，而不是让类型检查失败、整条回复丢失。
- **反爬验证页不再被当成正文**：`web.fetch` 识别微信「环境异常 / 去验证」页与 Cloudflare 人机验证页，返回可执行的错误（这是对方风控、不要重复请求同一 URL、改换来源或请用户手动打开），而不是把拒绝页交给模型分析。

### 4. 权限菜单误弹确认框

「自动审批」与「完全访问」两个选项都持久化 `mode=auto`，此前点「自动审批」也会弹出「正在切换到完全访问」的确认框。改为按选项标签判断。

## ✅ 验证

- `go build ./...`、`go vet ./...`、`gofmt -l` 干净
- **`go test ./...` 全量通过（含 `internal/tools/desktop` 的剪贴板权限测试）**，这是 v0.0.29 漏掉的那一层
- `scripts/check-comments.py` 零违规；`check-version-owner` 口径一致
- 新增回归测试 `TestGate_FullAccessStillConfirmsToolsWithoutPathsOnce`：完全访问仍需确认一次、会话信任后免审、高风险必审、剪贴板写入不可会话信任
- 桌面安装包内置 sidecar 实测 `gleam 0.0.30`，`doctor --strict` 通过

## ⬆️ 升级

- **桌面版**：启动后由内置更新检查提示，或直接下载 `Gleam-Setup.exe` 覆盖安装；用户数据（`~/.gleam`）不受影响。
- **CLI**：替换对应平台的二进制即可。
- 自更新与下载校验要求发布物带 `sha256` 摘要；缺失时拒绝安装并给出明确错误。

## ⚠️ 已知限制

微信文章正文无法通过 `web.fetch` 获取：需要登录态 cookie 或真实浏览器，属于 `docs/known-limits.md` 中刻意不做的范围。正确行为是明确告知用户，而不是假装抓到了内容。
