# 全仓 Google Style 规范落地 · 设计

- 日期：2026-10-11
- 状态：已批准。用户 2026-10-11 批准「**立标准 + 机械修全仓；Go 完全不动**」。
- 依据：<https://google.github.io/styleguide/>（按技术栈取对应语言的指南）

本文是这次全仓规范化的落地依据。实施步骤见实施计划（由 writing-plans 产出）。

## 一、现状实测

数据来源：`git ls-files` 计数、`gofmt -l ./internal ./pkg ./cmd`、按行统计「长度 > 80 字符」。

| 语言 | 文件 | 行数 | 现有 lint / format | > 80 列 |
| --- | ---: | ---: | --- | ---: |
| Go | 313 | 69,413 | **gofmt 全绿（0 个未格式化）** | 2,359 |
| JavaScript | 16 | 12,312 | 无 | 1,528 |
| CSS | 9 | 6,227 | 无 | 1,269 |
| Python | 30 | 4,361 | 无；5 个文件用 Tab 缩进 | 260 |
| HTML | 2 | 2,119 | 无 | 551 |
| TypeScript | 12 | 1,504 | 无（`tsc --strict` 已开） | 178 |
| Shell | 7 | 1,057 | 无 | 100 |
| JSON | 6 | — | 无 | — |
| YAML | 21 | — | 无 | — |
| Markdown | 15 | — | 无 | — |

补充事实：

- 根目录**没有** `package.json`（`commitlint.config.cjs` 悬空），也**没有任何** `.editorconfig` / eslint / prettier / ruff / stylelint 配置 —— 「标准」从未被写下来过。
- Python 已经是 92% 双引号（1137 双 vs 95 单），与 ruff / black 默认一致，机械修复的 churn 很小。
- 本机 **ruff / black / shfmt / shellcheck / prettier / eslint 全部未安装**，只有 `npx`。
- Python 分布：`scripts/` 15 个、`scripts/mutation/` 12 个、`desktop/spike/` 3 个。

**核心结论**：Go 这块已经达标。Google 的 Go 指南明确「Go 不设列宽限制，以 gofmt 为准」，313 个文件 gofmt 全绿即满足。真正的空白在 JS / TS / Python / CSS / HTML / Shell，以及「标准根本没写下来」这件事本身。

## 二、目标与非目标

### 目标

1. 把标准写成**可执行的配置文件**并接进 CI —— 这一步之后，新代码永久自动合规。
2. 存量代码按配置**机械修一遍**（缩进、列宽、引号、导入分组、行尾）。

### 非目标（明确不做）

- **Go 的 313 个文件一行不动**。gofmt 已全绿；且变异锚点大量指向 Go 源码，改它只有 churn、还会打掉负例控制。
- **不改注释语言**。代码注释用中文是 `CONTRIBUTING.md` 写明的既有约定，Google 指南不管注释语言。
- **不做语义改写**：命名、错误串大小写与标点、docstring / JSDoc **内容**，都不碰（用户已排除「语义规范一起改」这一档）。
- **不引 stylelint 的 Google 语义规则**，只做格式，不扩大依赖面。
- **不重排刻意写长的中文注释**。格式化器都不折行注释文本；同时 Python 侧显式 `ignore = ["E501"]`（见 4.1）。
- YAML / Markdown **没有**对应的 Google 公开指南，只由 `.editorconfig` 兜住缩进与行尾，不引入强制规则。

## 三、已核实的关键约束

动工前逐条核实过，任何一条被忽略都会产生「看起来成功」的失败：

1. **`.gitattributes` 是 `* -text`**：不做行尾转换，保证工作区字节 == 提交字节。因此**所有格式化器必须配置 `endOfLine = lf`**，否则会把 CRLF 写进工作区（提交后 `gofmt -l` 与文档预算会莫名变红）。
2. **变异锚点是精确字节串**。`scripts/mutation/*.py` 的每条 `edits` 的 `old` 是带 `\t` / `\n` 转义的字面量，必须在目标文件里**恰好出现一次**。非 Go 目标（按路径出现次数）：`scripts/check-*.py` 13 处（dom-anchors 4、api-docs 3、version-owner 2、py-utf8 / egress-owner / cue-owner / atomic-write 各 1）、`internal/webui/static/app.js` 3 处、`internal/webui/static/index.html` 1 处、`scripts/smoke-replay.sh` 1 处。**重排这些文件就会失配**。
3. **失配会被响亮拦住**：`check-mutation-anchors.py` 是 `verify.sh` 第 1 层的**硬门禁**，会逐条打印「哪个批次的哪条锚点出现 0 次 / 出现 2 次」。它是重排工作的**安全网**，不是障碍 —— 照着它报的行改批次里的 `old` 字面量即可。
4. **前端 `static/*.js` + `index.html` 走 `//go:embed`** 编进 sidecar，且被 5 个门禁按**源码文本**解析（dom-anchors / app-startup / version-owner / egress-owner / cue-owner）。改它们的格式要**连带重跑门禁 + 重编 sidecar + 重启**，并请用户验收（可视化改动）。
5. **`docs/` 门禁口径**：字数预算是**只告警不拦**（`check-doc-budget.py` 永远 exit 0）；`check-doc-refs.py` 只认 `§4.6.<数字>` 且设计文档本身不入库（本机不存在即跳过）。**本文件因此不含该形式的引用。**
6. **CI 目前完全没有 Node**（只有 Go 的 gofmt / vet / build / test 与 Python 门禁子集）。新增 lint job 需要 `actions/setup-node`；**新 job = 新 check 名**，是否纳入 required checks 由仓库 ruleset 决定（**用户操作**，agent 无仓库设置权限）。
7. **根 `node_modules` 会把一个 Go 包带进本模块的包树**。eslint → file-entry-cache → flat-cache → `flatted` 这个 npm 包**自带 Go 代码**（`node_modules/flatted/golang/pkg/flatted`），于是 `go list ./...` 会列出它。实测影响：`go vet ./...` 与 `go build ./...` 仍 exit 0；**CI 的 Go job 不跑 `npm ci`、看不到 node_modules，所以 CI 完全不受影响**；只有本机 `go test ./...` 多编译一个「无常测试文件」的包。已接受此副作用。退路：若将来某个依赖的 Go 包编不过，把 JS 工具链移进以 `.` 开头的目录（Go 的 `./...` 会跳过点目录）。
8. **本机 `internal/webui` 的 `TestGuard_*` 是既有 flake**（已存在文档化）。实测：有/无 node_modules 各跑都会间歇红（5 次里 2 绿 3 红），且与本次纯配置改动无关 —— 判 `verify.sh` 第 4 层红时先看是不是这一族。

## 四、设计

### 4.1 配置层（新增文件，全部在根目录）

| 文件 | 作用 |
| --- | --- |
| `.editorconfig` | 全仓基准：UTF-8 / LF / 尾行空行 / 去尾空格；Go = tab、Python = 4 空格、JS·TS·CSS·HTML·JSON·YAML·Shell = 2 空格 |
| `pyproject.toml` | ruff（Google Python 指南的落点）：`line-length = 80`、`target-version = "py312"`、`ignore = ["E501"]`。**E501 关掉的理由**：列宽只管代码，ruff formatter 已按 80 折代码，剩下的超长行基本全是刻意写长的中文注释与长字符串。pydocstyle 只保留**格式类**判据，要求补 docstring **内容**的规则不开（属语义，非目标） |
| `package.json`（private） | JS/TS 的**家**（根目录此前没有）。devDependencies 放 prettier + eslint（flat config）。脚本：`lint` / `format` / `format:check` |
| `.prettierrc.json` | Google JS/TS 口径：`printWidth 80`、`tabWidth 2`、`singleQuote true`、`semi true`、`endOfLine "lf"` |
| `.prettierignore` | 排除 `node_modules`、`desktop/dist`、`dist`、`bin`、生成物 |
| `eslint.config.mjs` | 只开**格式安全**规则：`no-var`、`prefer-const`（两者语义安全、可 `--fix`）；`eqeqeq` 只设为 **warn**——把 `==` 改成 `===` 是**语义改动**（全仓 16 处，分布在 app.js 4 / i18n.js 7 / settings2-pages.js 1 / settings2.js 4），属非目标，只报告不自动改。**不要求补 JSDoc**（属语义）；不设 `max-len`（注释刻意写长） |

Shell 的缩进（`indent_size = 2`、case 缩进）走 `.editorconfig`，shfmt 直接读它 —— 不额外加配置文件。

### 4.2 CI 层

在 `.github/workflows/ci.yml` 新增一个 job（与现有 Go job 并行）：

- `actions/setup-node` → `npm ci` → `prettier --check` → `eslint`；
- Python 侧 `ruff format --check` + `ruff check`；
- Shell 侧 `shfmt -d` + `shellcheck`。

该 job 只做**格式与静态**检查，与既有 Go job 并行、互不阻塞既有流程；是否纳入 required checks 由用户在 ruleset 决定（见约束 6）。

### 4.3 机械修复：分批

每批一个**独立分支 + PR**（master 禁 merge commit、要批准 + 签名），互不阻塞：

| 批 | 范围 | 文件数 | 耦合风险 |
| --- | --- | ---: | --- |
| **0** | 只落配置（4.1），**不动任何源码** | 6 个新文件 | 无。先立配置基线 |
| **1** | `desktop/src/**/*.ts` + `desktop/scripts/*.mjs` + `website/app.js` | 16 | 低（不进 embed、不改渲染） |
| **2** | `internal/webui/static/*.js` + `index.html` | 16 | **高**：5 个门禁按源码文本解析；3 条 + 1 条变异锚点；改完必须重编 sidecar + 用户验收 |
| **3** | `scripts/*.py` + `scripts/mutation/*.py` + `desktop/spike/*.py` | 30 | **中**：命中 13 条变异锚点，需同步改锚点字面量 |
| **4** | `scripts/*.sh` + `scripts/lib/*.sh` | 7 | 低-中：1 条锚点；shfmt 会动 `verify.sh` 的 heredoc，需逐处复核 |
| **5** | CSS(9) + `website/index.html` + JSON(6)（YAML / Markdown 不进 prettier） | 16 | CSS 渲染敏感（`03-align` / `05-consistency` 是照截图调的）；prettier 只动空白 = 渲染不变，但仍需重编验收 |
| **6** | CI job（4.2）+ `CONTRIBUTING.md` 补工具链一节 | 2 | 无。**必须最后**：全仓格式化完成前 CI 一落地就是红的 |

批序**必须顺序执行**，两条硬理由：

- **批 6 必须在最后**：CI 的 lint job 对着全仓跑，源码还没格式化时它就是红的 —— 一个从落地第一天就红的门禁等于没有门禁。
- **批 2 / 3 / 4 不能并行**：三者都会改 `scripts/mutation/batch-*.py` 里的锚点字面量（分别对应 `app.js`+`index.html` / `check-*.py` / `smoke-replay.sh`），**文件重叠**，并行必然冲突。

### 4.4 每批的验证动作

固定动作，逐批执行：

1. `bash scripts/verify.sh --quick`（第 1~4 层）。
2. 全量 `bash scripts/verify.sh`（第 5~6 层，端到端与就绪）。
3. `python scripts/check-mutation-anchors.py .` —— **会逐条点名**哪条锚点烂了；照着把对应批次的 `old` 字面量改成新文本。
4. **批 2 / 批 5 额外**：重编 sidecar（`internal/webui/static` 走 `go:embed`）+ 重启壳 + 请用户验收界面。

## 五、风险与对策

| 风险 | 对策 |
| --- | --- |
| 全量重排打掉变异锚点 | 门禁会响亮报红（约束 3）；每批跑锚点自检，按报告逐条改 `old`。**Go 不动即避开 86 条（绝大多数）锚点** |
| 前端 JS 重排弄坏按文本解析的门禁 | 批 2 单列；每个门禁都带 `--self-test`，改完逐个跑；失败即回滚该文件 |
| CSS 重排改到渲染 | 只用 prettier（纯空白）；重编后请用户看真壳 |
| Windows 上写出 CRLF | 所有格式化器 `endOfLine = lf`；对照 `.gitattributes` |
| 新依赖污染仓库形状 | 只在根新增 `package.json` + `package-lock.json`（`node_modules` 已忽略）；不新增运行时依赖 |
| 新增 CI job 未进 required checks 而形同虚设 | 明确告知用户需在 ruleset 里加（约束 6） |

## 六、交付顺序与验收

1. 本文件（spec）→ 用户复核。
2. 实施计划（writing-plans）→ 用户复核。
3. 批 0 → 批 6，**顺序**逐批 PR；每批按 4.4 验收。
4. 全部合并后：`CONTRIBUTING.md` 补一节「格式与 lint 工具链」（现状只写了 gofmt），使标准对新人可见。
