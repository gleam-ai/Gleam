# Gleam v0.0.31 — 桌面版命令修复、提问卡归位、反爬可接管

> 桌面版里 `shell.exec` / `shell.persistent` 全线报 `exec: "cmd": executable file not found in $PATH`；`ask_user` 的提问卡被顶到会话最上方看不见；遇到微信验证墙时模型没有正确的接管路径。本版逐个修掉。

**版本**: v0.0.31
**日期**: 2026-11-05
**基线**: v0.0.30

## 📦 下载

| 文件 | 说明 |
| --- | --- |
| `Gleam-Setup.exe` | Windows 桌面安装包（NSIS，含 Electron 45.2.1、内置 Node/uv 运行时与 gleam v0.0.31 sidecar）；`latest.yml` + blockmap 供自动更新 |
| `gleam-windows-amd64.exe` | Windows CLI（控制台版） |
| `gleam-darwin-amd64` / `gleam-darwin-arm64` | macOS（Intel / Apple Silicon） |
| `gleam-linux-amd64` | Linux x86_64 |
| `SHA256SUMS.txt` | 全部资产校验和 |

## 🩹 本版修复

### 1. 桌面版所有 shell 工具不可用

```
命令启动失败: exec: "cmd": executable file not found in $PATH
```

Electron 启动的 sidecar 继承的 PATH 可能被安装器或安全软件裁剪，`exec.Command("cmd")` 连解释器都找不到。

现在按 `COMSPEC` → `%SystemRoot%\System32\cmd.exe` → `LookPath` 的顺序解析绝对路径，并保证子进程 PATH 含 `System32`，`curl`、`where`、`powershell` 这类内置命令也能解析。`shell.exec`、`shell.persistent`、`jobs.start` 三个入口共用同一套构造。

### 2. `ask_user` 提问卡跑到消息流顶部

卡片此前 `prepend` 到会话最前，长会话里直接被顶出视口。现在追加到会话末尾，并滚动到刚好可见的位置（不会打断向上翻历史的操作）。

### 3. 模型下拉切掉选中标记

长模型名以 `nowrap` 撑宽，把右侧勾选挤出裁剪区。模型名与 provider 改为可省略，勾选固定保留。

### 4. `computer_use` 无法接管反爬页面

`open_app` 的 `app_name` 此前会拒绝含 `&`、`?` 的字符串（当成命令分隔符），等于禁止传 URL。现在：

- `open_app` 接受 http(s) 网址，其它协议（如 `file://`）明确拒绝
- 工具描述写明「这不是 HTTP 抓取工具；网页被验证墙挡住时，用 open_app 打开浏览器，请用户完成验证，再 screenshot 读屏」

### 5. `web.fetch` 命中验证页时给出下一步

错误信息现在明确：这是对方风控、重试同一 URL 不会成功、改用 `computer_use` 打开浏览器让用户接管、或请用户直接粘贴正文。

### 6. 审批理由不再像「会一直问」

无路径工具首次审批时，理由追加「批准后本会话不再询问该工具」。完全访问解除的是路径限制，不是敏感工具的一次性确认；剪贴板写入始终每次都要批。

## ✅ 验证

- `go build ./...`、`go vet ./...`、`gofmt -l` 干净
- `scripts/check-comments.py` 零违规；`check-version-owner` 口径一致
- 新增 `TestComputerUse_OpenAppAcceptsURL`：带查询串的 http 链接可用、`file://` 被拒
- `internal/tools/shell`、`internal/agent/tools/cua`、`internal/harness/safety`、`internal/tools/web` 全过
- 桌面安装包内置 sidecar 实测 `gleam 0.0.31`，`doctor --strict` 通过

## ⬆️ 升级

- **桌面版**：启动后由内置更新检查提示，或直接下载 `Gleam-Setup.exe` 覆盖安装；用户数据（`~/.gleam`）不受影响。
- **CLI**：替换对应平台的二进制即可。

## ⚠️ 已知限制

微信文章正文仍不能通过 `web.fetch` 获取：需要登录态 cookie 或真实浏览器。正确路径是 `computer_use` 打开浏览器让用户接管，或由用户粘贴正文；Gleam 不会伪装成浏览器去绕过对方风控。
