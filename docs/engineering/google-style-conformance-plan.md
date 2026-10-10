# 全仓 Google Style 规范落地 · 实施计划

> **For agentic workers:** REQUIRED SUB-SKILL: 用 superpowers:subagent-driven-development 或 superpowers:executing-plans 逐任务执行。步骤用 `- [ ]` 复选框跟踪。

**Goal:** 把 Google Style Guides 按技术栈落成全仓可执行的配置 + CI，并对存量代码做一遍机械修复；Go 不动。

**Architecture:** 先立配置（.editorconfig / ruff / prettier / eslint），再按「不进 embed → 进 embed → Python → Shell → CSS」的风险从低到高顺序分批格式化；每批用仓库既有门禁 + 变异锚点自检当测试。CI 强制放在最后一批，保证它落地即绿。

**Tech Stack:** ruff（Python）、prettier + eslint + typescript-eslint（JS/TS）、shfmt + shellcheck（Shell）、`.editorconfig`（全仓基准）。

**Spec:** `docs/engineering/google-style-conformance.md`

## Global Constraints

以下为 spec 的全局要求，**每个任务都隐含包含**：

- `.gitattributes` 是 `* -text`：**所有格式化器必须输出 LF**，`endOfLine` 一律 `lf`。
- **Go 的 313 个文件一行不动。**
- **不改注释语言**（中文注释是既有约定）；**不做语义改写**（含 16 处 `eqeqeq`，只 warn）。
- 批序**严格顺序**：批 6（CI）必须最后；批 2/3/4 共享 `scripts/mutation/batch-*.py`，不可并行。
- 每批固定验证：`bash scripts/verify.sh --quick` → 全量 `bash scripts/verify.sh` → `python scripts/check-mutation-anchors.py .`。
- 提交**必须签名**：`git -c gpg.format=ssh -c user.signingkey="$HOME/.ssh/id_ed25519_signing_vyrnsynx" -c commit.gpgsign=true commit -s`。
- 不新增**运行时**依赖；JS 工具链只在根 `package.json` 的 devDependencies。
- 批 2 / 批 5 改完必须**重编 sidecar + 重启壳 + 请用户验收**（`internal/webui/static` 走 `go:embed`）。
- master 禁 merge commit、要批准 + 签名；每批一个分支 + PR，**顺序**合并（前一批合了再开下一批）。

---

### Task 1: 工具链本地安装

**Files:** 无仓库文件变更（只装本机工具）。

**Interfaces:**
- Produces: 本机可用的 `ruff` / `shfmt` / `npx prettier` / `npx eslint`，供后续所有任务调用。

- [ ] **Step 1: 装 Python 与 Shell 工具**

```bash
python -m pip install --user ruff
go install mvdan.cc/sh/v3/cmd/shfmt@latest
```

- [ ] **Step 2: 记录版本（写进批 0 的 PR 描述）**

```bash
python -m ruff --version
"$HOME/go/bin/shfmt" --version
```

Expected: 两条都打印版本号，如 `ruff 0.14.x` / `v3.x.x`。

- [ ] **Step 3: 确认 shfmt 在 PATH 上**

Run: `shfmt --version`
Expected: 打印版本。**若 `command not found`**，后续一律用绝对路径 `"$HOME/go/bin/shfmt"`。

- [ ] **Step 4: 编译产出不入库**

`node_modules/` 已在 `.gitignore`；确认 `git status --porcelain` 里**没有**新增未跟踪的工具产物。

---

### Task 2: 配置层（批 0）

**Files:**
- Create: `.editorconfig`
- Create: `pyproject.toml`
- Create: `package.json`
- Create: `.prettierrc.json`
- Create: `.prettierignore`
- Create: `eslint.config.mjs`
- Create: `package-lock.json`（由 `npm install` 生成）

**Interfaces:**
- Produces: 全仓格式标准；后续每批只调用 `npx prettier` / `python -m ruff` / `shfmt`，不再新增配置。

- [ ] **Step 1: 建分支**

```bash
git switch -c style/google-conformance-config
```

- [ ] **Step 2: 写 `.editorconfig`**

```ini
# 全仓基准。所有格式化器都必须输出 LF：.gitattributes 是 `* -text`，
# 工作区字节必须与提交字节一致。见 docs/engineering/google-style-conformance.md。
root = true

[*]
charset = utf-8
end_of_line = lf
insert_final_newline = true
trim_trailing_whitespace = true

[*.go]
indent_style = tab

[*.py]
indent_style = space
indent_size = 4

[*.{js,mjs,cjs,ts,css,html,json,yml,yaml,sh}]
indent_style = space
indent_size = 2

# Markdown 行尾两个空格是换行，不能去。
[*.md]
trim_trailing_whitespace = false
```

- [ ] **Step 3: 写 `pyproject.toml`（ruff）**

```toml
[tool.ruff]
line-length = 80
target-version = "py312"

[tool.ruff.lint]
# 只开导入排序（机械、Google Python 指南明确要求分组）。
# E/F/W/D 一族留作后续单独评估：本仓的 `import _utf8  # noqa: F401`
# 正是 F401 会误报的副作用导入；「补 docstring 内容」属语义改写，已列为非目标。
select = ["I"]

[tool.ruff.format]
quote-style = "double"
line-ending = "lf"
```

- [ ] **Step 4: 写 `package.json`（根，private）**

```json
{
  "name": "gleam-workspace",
  "private": true,
  "description": "Repo-wide JS/TS formatting and lint tooling (Google style). Not published.",
  "scripts": {
    "format": "prettier --write .",
    "format:check": "prettier --check .",
    "lint": "eslint ."
  },
  "devDependencies": {
    "@typescript-eslint/parser": "^8.0.0",
    "eslint": "^9.0.0",
    "prettier": "^3.0.0",
    "typescript": "^5.9.0",
    "typescript-eslint": "^8.0.0"
  }
}
```

- [ ] **Step 5: 写 `.prettierrc.json`**

```json
{
  "printWidth": 80,
  "tabWidth": 2,
  "useTabs": false,
  "semi": true,
  "singleQuote": true,
  "trailingComma": "all",
  "endOfLine": "lf"
}
```

- [ ] **Step 6: 写 `.prettierignore`**

```
# 生成物 / 依赖
node_modules/
dist/
bin/
desktop/dist/
desktop/build/
desktop/build-*/
desktop/dist2/
website/dist/
package-lock.json

# 无对应 Google 指南，且 churn 巨大（CHANGELOG 235 KB）
*.md
*.yml
*.yaml
```

- [ ] **Step 7: 写 `eslint.config.mjs`**

```js
import tseslint from 'typescript-eslint';

// 只开语义安全、可自动修的规则。eqeqeq 只 warn：
// 把 == 改成 === 是语义改动（全仓 16 处），属非目标。
export default tseslint.config(
  {
    ignores: [
      'node_modules/**',
      'dist/**',
      'bin/**',
      'desktop/dist/**',
      'desktop/build/**',
      'website/dist/**',
    ],
  },
  {
    files: ['**/*.js', '**/*.mjs', '**/*.cjs', '**/*.ts'],
    extends: [tseslint.configs.base],
    rules: {
      'no-var': 'error',
      'prefer-const': 'error',
      eqeqeq: ['warn', 'always'],
    },
  },
);
```

- [ ] **Step 8: 装 JS 工具链**

```bash
npm install
```

Expected: 生成 `package-lock.json`，`node_modules/` 被 gitignore 忽略。

- [ ] **Step 9: 验证工具真的能跑（不是只看装上了）**

```bash
npx prettier --version
npx eslint --version
```

Expected: 分别打印 `3.x` / `9.x`。

- [ ] **Step 10: 确认本批**没有**碰任何源码**

```bash
git status --porcelain
```

Expected: 只有 6 个新文件 + `package-lock.json`，**没有任何既有源码**被改。

- [ ] **Step 11: 跑仓库门禁（配置不该影响它）**

Run: `bash scripts/verify.sh --quick`
Expected: 第 1~4 层全绿（配置不参与 Go 门禁）。

- [ ] **Step 12: 提交 + PR**

```bash
git add .editorconfig pyproject.toml package.json package-lock.json \
        .prettierrc.json .prettierignore eslint.config.mjs \
        docs/engineering/google-style-conformance.md \
        docs/engineering/google-style-conformance-plan.md
git -c gpg.format=ssh -c user.signingkey="$HOME/.ssh/id_ed25519_signing_vyrnsynx" \
    -c commit.gpgsign=true commit -s -m "build(style): add repo-wide Google style toolchain config"
git push -u origin style/google-conformance-config
gh pr create --title "build(style): add repo-wide Google style toolchain config" \
  --body "批 0：只落配置，不动源码。依据 docs/engineering/google-style-conformance.md。"
```

---

### Task 3: 批 1 — desktop TS + website JS

**Files:**
- Modify: `desktop/src/**/*.ts`（12）、`desktop/scripts/*.mjs`（3）、`website/app.js`（1）

**Interfaces:**
- Consumes: Task 2 的配置与工具链。
- Produces: 首批已格式化源码，验证工具链端到端可用。

- [ ] **Step 1: 建分支（在批 0 合并之后）**

```bash
git switch master && git pull
git switch -c style/google-conformance-batch1
```

- [ ] **Step 2: 跑检查器，先看它**失败**（这就是本批的测试）**

```bash
npx prettier --check "desktop/src/**/*.ts" "desktop/scripts/*.mjs" "website/app.js"
```

Expected: FAIL，列出待格式化文件。

- [ ] **Step 3: 格式化**

```bash
npx prettier --write "desktop/src/**/*.ts" "desktop/scripts/*.mjs" "website/app.js"
npx eslint --fix "desktop/src/**/*.ts" "desktop/scripts/*.mjs" "website/app.js"
```

- [ ] **Step 4: 检查器转绿**

```bash
npx prettier --check "desktop/src/**/*.ts" "desktop/scripts/*.mjs" "website/app.js"
```

Expected: `All matched files use Prettier code style!`

- [ ] **Step 5: 看 eslint 还剩什么（应只剩 0 个 error + 可能的 eqeqeq warn）**

```bash
npx eslint "desktop/src/**/*.ts" "desktop/scripts/*.mjs" "website/app.js"
```

Expected: 无 error。若出现 `prefer-const` error 未修完，重跑 `--fix`。

- [ ] **Step 6: 确认 TS 仍能编译（本批唯一的功能风险）**

```bash
cd desktop && npm run typecheck && cd ..
```

Expected: 无输出、退出码 0。

- [ ] **Step 7: 逐批门禁**

```bash
bash scripts/verify.sh --quick
bash scripts/verify.sh
python scripts/check-mutation-anchors.py .
```

Expected: 全绿；锚点自检通过（本批不碰锚点目标文件）。

- [ ] **Step 8: 提交 + PR**

```bash
git add desktop/src website/app.js
git -c gpg.format=ssh -c user.signingkey="$HOME/.ssh/id_ed25519_signing_vyrnsynx" \
    -c commit.gpgsign=true commit -s -m "style(desktop): format TypeScript and website JS per Google style"
```

---

### Task 4: 批 2 — webui static JS + index.html（高风险）

**Files:**
- Modify: `internal/webui/static/*.js`（15）、`internal/webui/static/index.html`（1）
- Modify: `scripts/mutation/batch-*.py`（锚点字面量，按门禁报告改）

**Interfaces:**
- Consumes: Task 2 配置。
- Produces: 已格式化前端；**必须**重编 sidecar 并经用户验收。

- [ ] **Step 1: 建分支**

```bash
git switch master && git pull
git switch -c style/google-conformance-batch2
```

- [ ] **Step 2: 记录改前的门禁基线**

```bash
python scripts/check-dom-anchors.py .
python scripts/check-app-startup.py .
python scripts/check-cue-owner.py .
python scripts/check-egress-owner.py .
python scripts/check-version-owner.py .
```

Expected: 全绿。这是"改之前是好的"的证据。

- [ ] **Step 3: 格式化**

```bash
npx prettier --write "internal/webui/static/*.js" "internal/webui/static/index.html"
npx eslint --fix "internal/webui/static/*.js"
```

- [ ] **Step 4: 重跑那 5 个门禁**

```bash
python scripts/check-dom-anchors.py --self-test .
python scripts/check-app-startup.py --self-test .
python scripts/check-cue-owner.py --self-test .
python scripts/check-egress-owner.py --self-test .
python scripts/check-version-owner.py --self-test .
```

Expected: 全绿。**若某个红**：说明 prettier 拆行破坏了该门禁的文本模式 —— 用 `git checkout` 回滚该单个文件，单独处理（不要继续）。

- [ ] **Step 5: 跑锚点自检，按报告改锚点**

```bash
python scripts/check-mutation-anchors.py .
```

Expected: 报出 `app.js` / `index.html` 相关的失配条目（形如 `batch-x.py → 出现 0 次`）。逐条打开对应 `scripts/mutation/batch-*.py`，把该条 `edits` 的 `old` 字面量改成**新格式下的等价文本**（只改空白/换行，**不改语义**）。

- [ ] **Step 6: 锚点复验至通过**

```bash
python scripts/check-mutation-anchors.py .
```

Expected: `变异锚点自检通过：N 批 / M 个变异`。

- [ ] **Step 7: 门禁**

```bash
bash scripts/verify.sh --quick && bash scripts/verify.sh
```

- [ ] **Step 8: 重编 sidecar 并请用户验收**

```bash
cd desktop && npm run build && cd ..
```

Expected: 构建成功。**然后请用户看真界面**（`internal/webui/static` 走 `go:embed`，不重编看不到变化）。

- [ ] **Step 9: 提交 + PR（用户验收通过后）**

---

### Task 5: 批 3 — Python（scripts + mutation + spike）

**Files:**
- Modify: `scripts/*.py`（15）、`scripts/mutation/*.py`（12）、`desktop/spike/*.py`（3）
- Modify: `scripts/mutation/batch-*.py`（锚点字面量，按报告改）

**Interfaces:**
- Consumes: Task 2 的 `pyproject.toml`。
- Produces: 已格式化 Python；锚点已重新对齐。

- [ ] **Step 1: 建分支**

```bash
git switch master && git pull
git switch -c style/google-conformance-batch3
```

- [ ] **Step 2: 先看规模（决定是否收窄 lint 面）**

```bash
python -m ruff check scripts/ desktop/spike/ --statistics
```

Expected: 只有 `I001`（导入未排序）一类。**若出现其它类别**：本批只做 `ruff format`，并把这些类别记进 follow-up（属"语义"，非目标），不要顺手改。

- [ ] **Step 3: 格式化 + 导入排序**

```bash
python -m ruff format scripts/ desktop/spike/
python -m ruff check --fix scripts/ desktop/spike/
```

- [ ] **Step 4: 复验干净**

```bash
python -m ruff format --check scripts/ desktop/spike/
python -m ruff check scripts/ desktop/spike/
```

Expected: 两条都干净。

- [ ] **Step 5: 门禁（Python 改了，第 1 层最相关）**

```bash
bash scripts/verify.sh --quick
```

Expected: 全绿。

- [ ] **Step 6: 跑锚点自检，按报告改锚点**

```bash
python scripts/check-mutation-anchors.py .
```

Expected: 报出 `check-*.py` 的失配条目。逐条把对应批次的 `old` 改成新文本（只动空白/引号/换行）。重复到通过。

- [ ] **Step 7: 全量门禁**

```bash
bash scripts/verify.sh
```

- [ ] **Step 8: 提交 + PR**

---

### Task 6: 批 4 — Shell

**Files:**
- Modify: `scripts/*.sh`（5）、`scripts/lib/*.sh`（2）

**Interfaces:**
- Consumes: Task 2 的 `.editorconfig`（shfmt 读它取缩进）。
- Produces: 已格式化 Shell。

- [ ] **Step 1: 建分支**

```bash
git switch master && git pull
git switch -c style/google-conformance-batch4
```

- [ ] **Step 2: 先看 diff 规模，重点盯 heredoc**

```bash
shfmt -d -i 2 -ci scripts/*.sh scripts/lib/*.sh
```

Expected: 打印 diff。**逐处确认 `verify.sh` / `smoke.sh` 的 heredoc 与 `<<'EOF'` 内容没被改写。**

- [ ] **Step 3: 落盘**

```bash
shfmt -w -i 2 -ci scripts/*.sh scripts/lib/*.sh
```

- [ ] **Step 4: 复验**

```bash
shfmt -d -i 2 -ci scripts/*.sh scripts/lib/*.sh
```

Expected: 无 diff。

- [ ] **Step 5: 锚点自检（`smoke-replay.sh` 有 1 条）**

```bash
python scripts/check-mutation-anchors.py .
```

- [ ] **Step 6: 门禁 + 冒烟（改的就是冒烟脚本，必须真跑）**

```bash
bash scripts/verify.sh
```

- [ ] **Step 7: 提交 + PR**

---

### Task 7: 批 5 — CSS + JSON

**Files:**
- Modify: `internal/webui/static/*.css`（8）、`website/style.css`（1）、`website/index.html`（1）
- Modify: `desktop/package.json`、`desktop/tsconfig.json`、`editor-plugin/gleam.plugin.json`、`internal/eval/*.json`
- Modify: `scripts/mutation/batch-*.py`（若有锚点命中）

**Interfaces:**
- Consumes: Task 2 配置。
- Produces: 已格式化 CSS/JSON；`internal/webui/static` 的 CSS 改动需用户验收。

- [ ] **Step 1: 建分支**

```bash
git switch master && git pull
git switch -c style/google-conformance-batch5
```

- [ ] **Step 2: 格式化**

```bash
npx prettier --write "internal/webui/static/*.css" "website/style.css" "website/index.html" \
  desktop/package.json desktop/tsconfig.json editor-plugin/gleam.plugin.json \
  "internal/eval/*.json"
```

- [ ] **Step 3: 确认 CSS diff 只有空白（渲染不变的关键证据）**

```bash
git diff --ignore-all-space --stat -- "*.css"
```

Expected: **空输出** = CSS 改动全是空白，渲染不可能变。

- [ ] **Step 4: 确认 JSON 仍是合法 JSON**

```bash
for f in desktop/package.json desktop/tsconfig.json editor-plugin/gleam.plugin.json internal/eval/cases.json internal/eval/baseline-select.json; do
  python -c "import json,sys; json.load(open(sys.argv[1], encoding='utf-8')); print('ok', sys.argv[1])" "$f"
done
```

- [ ] **Step 5: 锚点自检 + 门禁**

```bash
python scripts/check-mutation-anchors.py .
bash scripts/verify.sh --quick && bash scripts/verify.sh
```

- [ ] **Step 6: 重编 sidecar + 请用户验收（CSS 进 embed）**

```bash
cd desktop && npm run build && cd ..
```

- [ ] **Step 7: 提交 + PR**

---

### Task 8: 批 6 — CI 强制 + 文档

**Files:**
- Modify: `.github/workflows/ci.yml`（新增一个 job）
- Modify: `CONTRIBUTING.md`（补「格式与 lint 工具链」一节）
- Modify: `docs/engineering/google-style-conformance.md`（状态改为已落地）

**Interfaces:**
- Consumes: 前 5 批已全绿 —— 本批落地时 CI **必须立刻是绿的**。
- Produces: 永久自动合规。

- [ ] **Step 1: 建分支**

```bash
git switch master && git pull
git switch -c style/google-conformance-ci
```

- [ ] **Step 2: 本地预演 CI 会跑的全部命令（必须全绿才动 workflow）**

```bash
npx prettier --check .
npx eslint .
python -m ruff format --check .
python -m ruff check .
shfmt -d -i 2 -ci scripts/*.sh scripts/lib/*.sh
```

Expected: **全部干净**。任何一条不干净 → 回到对应批次修，不要继续。

- [ ] **Step 3: 给 `.github/workflows/ci.yml` 加 job**

在 `jobs:` 下新增（与既有 job 并列）：

```yaml
  style:
    name: Style (JS / TS / Python / Shell)
    runs-on: ubuntu-latest
    steps:
      - name: Checkout
        uses: actions/checkout@v7
        with:
          persist-credentials: false

      - name: Setup Node
        uses: actions/setup-node@v4
        with:
          node-version: "22"
          cache: npm

      - name: Setup Python
        uses: actions/setup-python@v7
        with:
          python-version: "3.12"

      - name: Install tools
        run: |
          npm ci
          python -m pip install ruff
          go install mvdan.cc/sh/v3/cmd/shfmt@latest

      - name: Prettier
        run: npx prettier --check .

      - name: ESLint
        run: npx eslint .

      - name: Ruff format
        run: python -m ruff format --check .

      - name: Ruff check
        run: python -m ruff check .

      - name: shfmt
        run: "$(go env GOPATH)/bin/shfmt" -d -i 2 -ci scripts/*.sh scripts/lib/*.sh

      - name: ShellCheck
        run: |
          sudo apt-get update && sudo apt-get install -y shellcheck
          shellcheck scripts/*.sh scripts/lib/*.sh
```

- [ ] **Step 4: 把新 job 接进 `ci-success` 的 `needs`**

把 `needs: [format-vet-build, test]` 改成 `needs: [format-vet-build, test, style]`，并在下面那个 `for r in ...` 循环里加上 `"${{ needs.style.result }}"`。**不加这步 = 门禁形同虚设。**

- [ ] **Step 5: 本地跑一遍既有门禁确认没连带破坏**

```bash
bash scripts/verify.sh
```

- [ ] **Step 6: `CONTRIBUTING.md` 补一节**

在「格式」相关内容处补入工具链与命令（安装方式、`npm run format` / `format:check` / `lint`、`python -m ruff`、`shfmt`、`.editorconfig` 的作用、`eqeqeq` 只 warn 的理由）。

- [ ] **Step 7: 更新 spec 状态**

把 `docs/engineering/google-style-conformance.md` 的「状态」改为「已落地」并补一行落实日期。

- [ ] **Step 8: 提交 + PR**

- [ ] **Step 9: 告知用户需要在仓库 ruleset 里把新 job 加进 required checks**（agent 无仓库设置权限）。

---

## Self-Review

- **Spec 覆盖**：4.1 配置层 → Task 2；4.2 CI → Task 8；4.3 逐批 → Task 3~7；4.4 每批验证 → 每个 Task 的门禁步骤；约束 1~6 → Global Constraints + 各 Task 的针对性检查。无遗漏。
- **类型/命名一致**：全计划只用 `prettier` / `eslint` / `ruff` / `shfmt` / `check-mutation-anchors.py` 这几个名字，前后一致。
- **占位符扫描**：无 TBD/TODO；每个命令都可直接执行。
- **已知的范围外**：`eqeqeq` 16 处、`E/F/W/D` lint 一族 —— 均在 spec 里写明为非目标，本计划不碰。
