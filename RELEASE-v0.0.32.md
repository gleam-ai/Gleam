# Gleam v0.0.32 — 更新失败的提示说人话，下载依旧只走 GitHub

> 更新检查失败时，用户看到的是一整串 `dial tcp …` 与 HTTP 响应头，既看不懂也分不清是自己网络的问题还是 Gleam 坏了。本版把三端的错误文案收口到一句可读的话，并明确了下载来源。

**版本**: v0.0.32
**日期**: 2026-11-05
**基线**: v0.0.31

## 📦 下载

| 文件 | 说明 |
| --- | --- |
| `Gleam-Setup.exe` | Windows 桌面安装包（NSIS，含 Electron 45.2.1、内置 Node/uv 运行时与 gleam v0.0.32 sidecar）；`latest.yml` + blockmap 供自动更新 |
| `gleam-windows-amd64.exe` | Windows CLI（控制台版） |
| `gleam-darwin-amd64` / `gleam-darwin-arm64` | macOS（Intel / Apple Silicon） |
| `gleam-linux-amd64` | Linux x86_64 |
| `SHA256SUMS.txt` | 全部资产校验和 |

## 🩹 本版修复

### 1. 引擎：连不上更新源时说明是谁的问题

```
检查更新失败：Get "https://api.github.com/repos/...": dial tcp 20.205.243.168:443: connectex: ...
```

现在：

```
连不上 GitHub 更新源：dial tcp 20.205.243.168:443: connectex: ...。这是本机网络问题，不是 Gleam 故障；稍后重试，或到发布页手动下载
```

新增 `netErrSummary`：`url.Error` 只保留底层原因，超时统一表述成「请求 GitHub 超时（本机网络到 api.github.com 太慢或不通）」，不再把完整请求 URL 带进界面。CLI 自更新与桌面更新源两条路径共用同一句口径。

### 2. 桌面端：不再把整个请求塞进弹窗

`electron-updater` 的 `HttpError.message` 会把 URL 和响应头一起序列化成一段多行文本。现在 `updateErrorText` 只取「状态码 + 引擎自己的说明」：

```
502 Bad Gateway · 连不上 GitHub 更新源：…
```

HTML 错误页整段丢弃（只留状态码），非 HTTP 错误兜底取首行并截断到 200 字；下载失败回传给渲染层的文案同样走它，主进程日志仍保留完整错误用于排查。

### 3. 弹窗：同一句建议不再说两遍

后端文案已含「稍后重试，或到发布页手动下载」，而弹窗本身就有「去发布页」按钮。渲染时先去掉消息末尾句号，再判断消息里是否已有补救建议，避免出现「……手动下载。可以稍后再试，或去发布页手动下载。」这种复读。

## 🔒 下载来源

- 桌面更新源地址是本机引擎（`127.0.0.1`），它**只是传输层**：`electron-updater` 的 `generic` feed 指向 `/api/update/desktop`，引擎用 `api.github.com` 取发布信息，再流式转发 release 资产（支持 Range 断点）。
- 引擎只转发**最新发布里确实存在**的资产名，其它名字一律 404；资产地址必须命中 `github.com` / `objects.githubusercontent.com` / `release-assets.githubusercontent.com` 之一。
- `latest.yml` 里的 `url` 是相对名 `Gleam-Setup.exe`，不含任何本机路径或内网地址。
- 引擎自更新（CLI）直接走 GitHub 资产地址，不经过本机转发。

## ✅ 验证

- `go build ./...`、`go vet ./...`、`gofmt -l` 干净
- `go test ./internal/webui -count=1` 全过；新增 `TestNetErrSummaryKeepsCauseAndDropsRequestDetail`：`url.Error` 只留底层原因、超时可读、不泄漏完整请求 URL
- 桌面 `npm test` 5 条全过（新增：`HttpError` 只显示状态与引擎说明且不含 `Headers`/token；HTML 错误体不进弹窗）；`npm run typecheck` 无错误
- `python scripts/check-version-owner.py`：应用内前端无版本字面量，官网徽标与 owner 一致
- `prettier --check`：改动的前端文件格式一致

## ⬆️ 升级

- **桌面版**：启动后由内置更新检查提示，或直接下载 `Gleam-Setup.exe` 覆盖安装；用户数据（`~/.gleam`）不受影响。
- **CLI**：替换对应平台的二进制即可。

## ⚠️ 已知限制

更新检查依赖 `api.github.com` 的可达性。本机网络若被劫持/重置，界面会明确告知这是本机网络问题并可去发布页手动下载——Gleam 不会绕过、也不会伪装成其它流量。
