# Gleam v0.0.28 更新日志 — 融入 DeepSeek Harness 可借鉴能力

**发布日期**: 2026-11-05
**版本**: v0.0.28
**验证**: `scripts/verify.sh` 六层全过；真实模型（DeepSeek）全链路实测五项基础能力——模型发送、代码编写（写码→跑码）、定时任务（创建→触发→归档）、computer_use 截屏（真实 PNG）、image.generate（mock 端到端真实落盘）；发布二进制 `doctor --strict` 与 `eval --layer smoke --strict` 实测通过

## 🎯 本次更新的由来

通读 DeepSeek Harness（dsh）社区文档站（deepseekdocs.com）全部 60 页后逐项对照评测：Gleam 已有能力（工具输出溢出、headless 契约、多模型分档、MCP 客户端、定时调度、人机问答、fail-closed 审批）保持不动；dsh 的 JS 插件运行时、遥测、PTY 全真终端判定为**刻意不做**（口径见 `docs/known-limits.md`）；以下四项能力融入。

## ✨ 新增能力

### 1. 原生 SKILL.md 指导技能（渐进披露）

- 自动发现工作区 `.gleam/skills`、`.agents/skills` 与用户根下的 `<name>/SKILL.md`（含 `<name>.md` 平铺形态），改完即生效
- frontmatter 校验：name（kebab-case）、description（必填）、`when_to_use`、`disable-model-invocation`
- `skill.list` 合并展示两类技能（`source: step | markdown`），新增 **`skill.open`** 工具：先看摘要、按需打开全文，模型按指南自行执行
- 与既有步骤技能（可重放的固定工具序列）互补：指导技能承载"给模型读的操作文档"

### 2. 真子任务委派（agent.delegate mode=task）

- **task 模式**：先规划（步数封顶 6）、再真实调用工具（安全门禁/审计/溢出逐项生效）、最后汇总返回
- 5 分钟硬超时；嵌套深度沿用 `MaxDelegateDepth=2`；mock 模型下确定性汇总，离线自测可复现
- 原 **answer 模式**（单次 LLM 直答）保持默认，成本不变

### 3. 会话全文检索（session.search）

- 跨全部历史会话检索：多词 AND、大小写不敏感、命中计数 + 上下文片段 + 命中消息定位，确定性排序
- 新增只读工具 **`session.search`** 与端点 `GET /api/conversations/search`
- 刻意不建索引：全量扫描毫秒级，零失效、即改即生效

### 4. 工具生命周期钩子（safety.hooks）

- 配置 `safety.hooks`：`pre_tool` / `post_tool` × 工具过滤 × 命令；载荷经 stdin 传 JSON（防注入）
- **pre_tool exit 2 阻断**该次工具调用（其余退出码只告警——钩子自身故障不卡任务）；post_tool 纯通知
- 主任务 / 技能 / 子任务三条执行路径全部生效

## 📦 产物与校验

| 产物 | 说明 |
| :--- | :--- |
| `Gleam-Setup.exe`（152 MB） | Windows 桌面安装包（NSIS，含 Electron 45.2.1、内置 Node/uv 运行时与 gleam v0.0.28 sidecar）；`latest.yml` + blockmap 供自动更新 |
| `gleam-windows-amd64-v0.0.28.exe` | Windows CLI（控制台版） |
| `gleam-darwin-amd64-v0.0.28` / `gleam-darwin-arm64-v0.0.28` | macOS（Intel / Apple Silicon） |
| `gleam-linux-amd64-v0.0.28` | Linux x86_64 |
| `SHA256SUMS.txt` | 全部产物校验和 |

各二进制均经 Mach-O/ELF/PE 魔数校验；桌面安装包内置 sidecar 实测 `gleam 0.0.28`、`doctor --strict` 通过。

### 5. computer_use 桌面自动化（本轮补齐落地）

- 四种后端：`GLEAM_CUA_MODE`（direct/powershell/mcp/mock）> cua 二进制 > Windows 原生 powershell（user32 截屏/点击、剪贴板粘贴输入中文、Start-Process 启动）> 本地 MCP
- mock 模式诚实标注"未真实执行"，动作参数过注入校验；截屏与 `desktop.screenshot` 共用平台实现

### 6. image.generate 文生图（新增能力）

- OpenAI 兼容 `images/generations`（GLM cogview / OpenAI gpt-image-1 等）；b64/url 双响应形态；按魔数认格式落盘 `images/`
- 新配置 `llm.image_model`（设置页可改）；未配置/无密钥/主模型无图像能力时逐层明确报错；出网接安全台账

## 🔧 顺带修复

- **配置分层优先级**：settings.yaml 覆盖层曾把 `GLEAM_*` 环境变量与 `--workspace` 等CLI 标志的值悄悄冲掉（实测文件被写进旧目录）；现在覆盖层加载后重放环境变量与 CLI 标志，与文档口径一致
- **Windows 截屏恒败**（存量）：`powershell -Command script path` 的 `$args[0]` 拿不到参数 → 截屏必报解析错误；改为环境变量传路径
- **模型配置绑定**：厂商预设 + Key + 空 base_url 保存被拒（先按预设解析主机再入库）；openai_responses 历史 assistant 段改用 `output_text`（严格 OpenAI 否则 400）；新增多主机密钥隔离/三协议切换/流式/跨厂商辅助模型的绑定一致性测试，并以真实二进制对本地 mock 跑通 responses 与 anthropic 协议完整 goal
- 修复 `internal/webui/ask_todo_test.go` 引用不存在的测试辅助（`agent.WithTaskIDForTest` 已补导出）
- 全仓 `.go` 注释统一为 ASCII（中文只保留在字符串字面量），通过 `scripts/check-comments.py` 零违规
- README 端点清单补齐 3 个已注册端点（`/api/conversations/search`、`/api/questions/{id}`、`/api/todos/{task_id}`）

## 🩺 发版前全仓 review：接口、UI/UX 与安全

对引擎、WebUI 接口、前端交互、编辑器扩展、桌面壳与官网做了一轮完整审查，修掉以下问题。

### 会让功能直接不可用

- **提问卡从来没有接线**：SSE 的 `question` / `question_resolved` / `todos` / `tool` 四类事件前端没有任何监听，`ask_user` 对用户完全不可见、任务必然等到 5 分钟超时。现补提问卡（选项 / 自由输入 / 回车提交 / 失败重试 / 去重）、待办卡片与工具事件时间线；SSE 重连会重放待回答问题与待办快照。
- **远端 MCP 配置被丢弃**：`toMCPCfgs` 没有透传 `URL` / `Headers` / `Env`，且 `RegisterAll` 强制要求 `Command`，远端 server 完全不可用。
- **扩展默认 taskMode 非法**：`gleam.defaultTaskMode` 默认 `auto` 不是合法 task_mode，`goal/submit` 恒被拒绝。
- **配置文件静默丢字段**：`SaveOverlay` 会抹掉 `trusted_paths` / `trusted_tools` / `hooks`；`LoadOverlay` 解析失败时静默回默认配置。现保留字段，损坏时明确告警并备份。

### 安全

- `image.generate` 返回的图片 URL 未做私网校验（SSRF）：现复用 `web.fetch` 的 `GuardURL`，并对重定向逐跳校验、独立 client 做 DNS pinning。
- **自更新在发布物缺 `sha256` 摘要时跳过校验直接安装**：改为 fail-closed 拒绝，并给出明确错误。
- 桌面壳 CSP 允许任意远程 iframe：收紧为 `frame-src 'none'`，`img-src` 去掉 `https:`。
- `hook` 命令输出无上限可撑爆内存：改为 64 KB 有界缓冲。
- `go-status` 执行任意用户指定路径无超时：加 5 秒上下文超时。

### 稳定性

- `auth` 两处 `http.NewRequest` 错误被丢弃导致 nil panic；`app.go` 的 `lastSeen` 数据竞争与无超时 `http.Post`。
- `shell.exec` 的 `timeout<=0` 变成立即取消；`file.search` 命中 200 条被判失败并吞掉遍历错误；`web.fetch` 按字节截断会切碎 UTF-8 且吞读取错误。
- WebUI：`writeJSON` 序列化失败仍发 200 半截 body；SSE 无写超时；`show-window` goroutine panic 会带走进程；daemon 无 body 上限且回显内部路径；Go 工具链安装拿不到官方校验和仍解压执行。
- 编辑器扩展：spawn 失败后任务永久 `running`；stdin EPIPE 后残留孤儿引擎；提交失败无任何提示。

### UI / UX

- **默认主题统一为暗底**：`color-scheme`、`theme-color` 与首帧引导三处口径一致，与官网同一套视觉。
- GEO：开关事件绑定失效、清空无二次确认、失败静默、长任务无反馈；OAuth 重复点击产生多个轮询器；线索面板 `display` / `hidden` 双轨互斥；批量删除吞错。
- 官网：中文页补齐 8 个未定义 class、移动端导航不再整体消失、外链补 `rel`、无效的 `go install` 命令改为 clone + build、微光收敛到规范允许的 5 处、中英下载页补齐 macOS Apple Silicon / Intel 两个架构。

### 闸门自身

- `verify.sh` 的 `gofmt` 只看输出不看退出码（工具缺失也判绿）；`smoke.sh` 丢掉管道退出码且有重复断言；`ci.yml` 把 `skipped` 当通过；`package.sh` 把 `go vet` 错误吞掉。
