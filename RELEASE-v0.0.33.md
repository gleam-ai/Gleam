# Gleam v0.0.33 — 会话一次删掉，反爬页面不再空转

> 两个从真实使用里冒出来的问题：删除当前会话要点两次；一个微信推文链接让任务反复重规划二十多轮，最后只回一句步骤计数。

**版本**: v0.0.33
**日期**: 2026-11-06
**基线**: v0.0.32

## 📦 下载

| 文件 | 说明 |
| --- | --- |
| `Gleam-Setup.exe` | Windows 桌面安装包（NSIS，含 Electron 45.2.1、内置 Node/uv 运行时与 gleam v0.0.33 sidecar）；`latest.yml` + blockmap 供自动更新 |
| `gleam-windows-amd64.exe` | Windows CLI（控制台版） |
| `gleam-darwin-amd64` / `gleam-darwin-arm64` | macOS（Intel / Apple Silicon） |
| `gleam-linux-amd64` | Linux x86_64 |
| `SHA256SUMS.txt` | 全部资产校验和 |

## 🩹 本版修复

### 1. 删除会话一次就生效

后端第一次 `DELETE /api/conversations/{id}` 就已经删掉记录，且该操作幂等。问题在前端：删除**当前打开的会话**时只走 `exitConvoToWorkbench()`，它清 `currentConvo` 却不重绘 `#convo-list`；第二次点击时 `currentConvo === null`，才轮到 `loadConvoList()`。现在两个分支统一刷新列表。

### 2. 风控验证页不再让任务空转

`web.fetch` 认出微信/Cloudflare 验证页后，错误此前只是一句普通文本，被归类为 `ErrUnknown`：

- 反思器看到「有未通过但值得再试」就要求 `replan`
- 重规划每轮换工具/换参数，而打转熔断只认**完全相同的 `tool|args` 指纹**，所以永远不触发
- `depends_on` 让后续步骤机械跳过，`reply` 没执行 → 用户拿到的是「执行了 3 个步骤：成功 0，失败 1，跳过 2」

现在：

- 新增 `ErrNeedsHuman`，优先识别「风控 / 验证页 / 人机验证 / 请用户完成验证 / 请用户直接粘贴正文」
- 新增 `humanHandoff()`，在**反思之前**熔断：第一轮被这类障碍挡住且尚未给用户回复时，直接结束并返回可执行的下一步——把正文粘贴到对话里、在浏览器完成验证后重发、或换一个能访问的来源；不再反思、不再重规划，也避免幻觉的 `done` 把受阻任务判成成功
- `cueKindText` 补上该归因的中英文口径

> 边界：脚本抓微信正文这件事本身做不到，那是对方风控，换工具重试也不会成功。本版改的是「第一时间停下并说清要做什么」，而不是烧掉二十轮预算后给一句步骤计数。

### 3. 验证闸门不再被自己的测试卡住

`TestJobs_StartListOutput` 固定 `sleep 300ms` 等首行输出，全仓并发跑时 Windows 启动 `cmd.exe` 会超过这个时间，连续两次全量跑都挂在同一处。改成最多 5 秒轮询首字节，断言不变。

## 🔒 下载来源

与 v0.0.32 一致：桌面更新源地址是本机引擎（`127.0.0.1`），它只是**传输层**，`latest.yml` 里的 `url` 是相对名 `Gleam-Setup.exe`，安装包字节来自 GitHub release 资产（支持 Range 断点），引擎只转发最新发布里确实存在的资产名。

## ✅ 验证

- 真实 `web.fetch` 验证页测试断言错误归类为 `needs_human`；Agent 集成测试把 `MaxReplans=5`，实测 **1 次规划 / 0 次反思**、Summary 为人工接管文案、依赖步骤仍为 skipped
- Playwright 新增 `delete` 套件并做 A/B：旧逻辑报 `Locator expected to have count '0', Actual value: 1`，修复后 PASS；`--suite all` 全绿
- `go test ./...` 42 个包全过（本机 Windows 符号链接策略导致的 `TestFileTools_RejectsSymlinkEscape` 显式 `-skip`，CI 跑 Linux 不受影响）
- `gofmt -l` 无输出；`go vet`、`go build`、`check-comments`（0 违规）、`check-atomic-write / app-startup / dom-anchors / egress-owner / version-owner` 全过；`prettier` / `ruff` / `eslint` 无新增问题
- `gleam eval --layer smoke --strict` 10/10；`gleam doctor --strict` 通过

## ⬆️ 升级

- **桌面版**：启动后由内置更新检查提示，或直接下载 `Gleam-Setup.exe` 覆盖安装；用户数据（`~/.gleam`）不受影响。
- **CLI**：替换对应平台的二进制即可。
