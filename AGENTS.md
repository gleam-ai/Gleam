# Gleam · Agent 指引

> 改这里的门槛：能不能被一条 exit non-zero 的命令证明？

## 三条铁律

1. **一个事实一个 owner**：一件事只写在一处，别处只放指针。
2. **先问归属，再谈实现**：这条约束属于哪一层、哪个文件？
3. **改完必须能证明**：给出真跑过的命令与输出，而不是「应该没问题」。

## 布局

| 目录 | 装什么 |
| --- | --- |
| `cmd/gleam` | CLI 入口 |
| `internal/` | 引擎：agent / webui / tools / config / harness / buildinfo |
| `internal/webui/static` | 前端，`//go:embed` 编进 sidecar |
| `pkg/` | 可复用类型 |
| `desktop/` | Electron 壳 |
| `scripts/` | 闸门、冒烟、打包 |
| `docs/known-limits.md` | **刻意不做**的唯一口径 |

## 规则 → 命令

| 规则 | 命令 |
| --- | --- |
| 格式与引用 | `gofmt -l ./internal ./pkg ./cmd`；`scripts/check-*.py` |
| 静态检查 | `go vet ./...` |
| 编译 | `go build ./...` |
| 行为 | `go test ./... -count=1 -timeout 300s` |
| 端到端 | `bash scripts/smoke.sh`；`bash scripts/smoke-replay.sh` |
| 就绪与评测 | `bin/gleam.exe doctor --strict`；`bin/gleam.exe eval --layer smoke --strict` |

六层一次跑：`bash scripts/verify.sh`（`--quick` 只到第 4 层）。各层职责与「为什么是这个顺序」写在 `scripts/verify.sh` 头部。

## 风格

本仓遵循 [Google Style Guides](https://google.github.io/styleguide/)、**按语言**取对应指南，由 CI 的 `style` job 强制。语言 → 指南 → 执行工具的映射表、三处刻意偏离、以及本地命令，都在 `CONTRIBUTING.md`。改代码前先跑 `npm run format:check` 与 `python -m ruff format --check .`。

## 别的入口

- **变异批次的清单**在 `scripts/mutation/README.md`，这里只指到目录。
- **内部文档不入库**（`Gleam 技术设计文档.md`、`*对照与升级清单.md`、`CODE_REVIEW_REPORT.md`）：已提交的文件里**不许引用**它们，否则就是悬空指针。

## 三问自检

1. 知识外置了吗？——只在我脑子里，下次它就不存在。
2. 正确入口明确吗？——新人只读本文件，能不能找到该改哪。
3. 错误何时被发现？——有没有一条 exit non-zero 的命令，在最近的地方拦住它。

完整答案在 `docs/known-limits.md` 头部。
