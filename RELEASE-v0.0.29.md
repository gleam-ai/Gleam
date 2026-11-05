# Gleam v0.0.29 — 权限失控与回复丢失修复

> ⚠️ **本版已被 v0.0.30 取代，请勿使用。** 它让「完全访问」跳过了无路径中风险工具的一次性确认，其中包括 `desktop.clipboard.read`（可能带出密码）。CI 的权限测试拦住了这个回归，v0.0.30 已撤回该改动。本版保留的两项独立修复（`reply` 参数归一化、反爬验证页识别）在 v0.0.30 中继续有效。

> 一个把「完全访问」变成逐次审批的安全门控缺陷、一次导致最终回复整条丢失的参数校验失败，以及反爬验证页被当成正文喂给模型的问题。

**版本**: v0.0.29
**日期**: 2026-11-05
**基线**: v0.0.28

## 📦 下载

| 文件 | 说明 |
| --- | --- |
| `Gleam-Setup.exe` | Windows 桌面安装包（NSIS，含 Electron 45.2.1、内置 Node/uv 运行时与 gleam v0.0.29 sidecar）；`latest.yml` + blockmap 供自动更新 |
| `gleam-windows-amd64.exe` | Windows CLI（控制台版） |
| `gleam-darwin-amd64` / `gleam-darwin-arm64` | macOS（Intel / Apple Silicon） |
| `gleam-linux-amd64` | Linux x86_64 |
| `SHA256SUMS.txt` | 全部资产校验和 |

二进制为无版本后缀的固定名，`/releases/latest/download/<文件名>` 可以直接引用，自动更新也按这些名字匹配。

## 🩹 修复

### 1. 「完全访问」仍在逐次弹审批

安全门控只有对**能枚举路径**的工具（`PathAware`）在信任路径内才放行。`agent.delegate`、`web.fetch` 这类没有路径参数的工具会落到最后的兜底分支：

```
无法确认操作范围，需要人工确认
```

而「完全访问」（`mode=auto`）在设置页的承诺是「仅高风险操作需要批准」——文案与行为不一致。

现在：`mode=auto` 下非路径类的中风险工具直接放行；高风险（`shell.exec` 等）仍强制审批；用户显式把某工具覆盖为「需批准」时，更具体的设置优先。

### 2. 最终回复整条丢失

模型偶尔把 `reply.text` 传成段落数组或包裹对象（例如把 `agent.delegate` 的结果对象直接塞进来）。`toolutil.ValidateArgs` 按 schema 严格校验，`Execute` 根本不会执行，用户一条回复都收不到。

现在：新增可选接口 `types.ArgNormalizer`，`ValidateArgs` 在校验前调用；`ReplyTool` 实现它，数组按段落拼接、对象取 `text`/`content`/`answer`/`reply`/`message` 字段、支持嵌套。只对 `reply` 生效，不放宽其他工具的校验。

### 3. 反爬验证页被当成正文

微信公众平台对非浏览器请求返回「环境异常 / 完成验证后即可继续访问 / 去验证」页面。此前这页 HTML 会被提取成正文交给模型，模型于是花多轮去「分析」一个拒绝页，最后报告"未找到任何岗位信息"。

现在：`web.fetch` 识别微信验证页与 Cloudflare 人机验证特征，直接返回

```
目标站点返回了微信访问验证页，没有拿到正文。这是对方的风控策略，不是网络故障；
不要重复请求同一 URL，改换来源或请用户手动打开
```

判定需同时命中「环境异常」与「去验证」，讨论验证技术的正常文章不会被误吞。

### 4. 权限菜单误弹高危确认

「自动审批」与「完全访问」两个选项都持久化 `mode=auto`，但此前点「自动审批」也会弹出「正在切换到完全访问」的确认框。改为按选项标签判断。

## ✅ 验证

- `go build ./...`、`go vet ./...`、`gofmt -l` 干净
- `scripts/check-comments.py` 零违规
- 新增回归测试：完全访问放行无路径中风险工具、显式覆盖优先、`reply` 参数归一化（数组/包裹对象/嵌套）、验证页识别、讨论验证的文章不误判
- 受影响包 `internal/webui`、`internal/agent`、`internal/tools/web`、`internal/tools/std`、`internal/harness/safety`、`internal/tools/toolutil`、`pkg/types` 全部通过
- 桌面安装包内置 sidecar 实测 `gleam 0.0.29`，`doctor --strict` 通过

## ⬆️ 升级

- **桌面版**：启动后由内置更新检查提示，或直接下载 `Gleam-Setup.exe` 覆盖安装；用户数据（`~/.gleam`）不受影响。
- **CLI**：替换对应平台的二进制即可。
- 自更新与下载校验现在要求发布物带 `sha256` 摘要；缺失时拒绝安装并给出明确错误，而不是跳过校验。

## ⚠️ 已知限制

微信文章正文无法通过 `web.fetch` 获取：需要登录态 cookie 或真实浏览器，属于 `docs/known-limits.md` 中刻意不做的范围。正确行为是明确告知用户，而不是假装抓到了内容。
