# GLEAM DESIGN SYSTEM v0.1

> 微光 · Gleam — 本地优先的 AI 同事
> 本文件是**机器可执行的设计约束**。任何 AI 编码助手（Codex / Cursor / Claude Code / Kimi）在生成 Gleam 的页面、组件、文档排版前，必须先完整读取本文件并严格遵守。

---

## 0. 怎么用这份文档

**放入项目：**
```
gleam/
├── GLEAM-DESIGN.md     ← 本文件，放在仓库根目录
└── ...
```

**喂给 AI 助手：**
- Cursor / Codex：在 prompt 里写 `遵循 @GLEAM-DESIGN.md` 或把它加进 `.cursorrules`
- 对话式：把全文粘贴进对话，再说需求
- 与 `taste-skill` 配合：先加载 taste-skill（管布局/节奏/动效的通病），再用本文件覆盖它的默认值（管 Gleam 的身份）。**冲突时以本文件为准。**

**本文件的优先级：**
```
GLEAM-DESIGN.md  >  taste-skill 默认  >  框架默认样式
```

---

## 1. 定位锚点（一句话，所有决策回溯到这里）

> **Gleam 是一个靠谱的同事。用户得到的不是"更强的能力"，而是"被归还的时间"。**

推论：
- 视觉基调是**安静、克制、可信、有人味**，不是炫技、不是锋利、不是赛博
- 沟通重点是**"你不用管了"**，不是"看我多能干"
- 所有证据用**数字和痕迹**呈现，不用形容词堆砌
- 产品名"微光"是**物理事实**，直接决定了配色必须是暗底（见 §4）

---

## 2. 三条不可违背的原则

### 原则一：留白比装饰重要
Gleam 卖的是"时间被归还"。一个塞满的页面本身就在违背产品承诺。
**任何页面，内容区两侧和段落之间的留白只允许加，不允许减。**

### 原则二：微光只在暗处成立
高亮色全站占比 **< 5%**。宁可少，不可多。
一旦你觉得"这里好像需要再亮一点"，答案是**不**。

### 原则三：可信来自痕迹，不来自形容
写"智能""强大""高效"不如写"94/100""23 个文件""0 外部依赖"。
**能用数字表达的，禁止用形容词表达。**

---

## 3. 色彩系统

### 3.1 Token 表

| Token | 值 | 用途 |
|---|---|---|
| `--bg` | `#0E1014` | 页面底色（深灰蓝，**禁止 #000**） |
| `--raised` | `#14171D` | 卡片、浮层 |
| `--raised2` | `#181C23` | 卡片内嵌层、hover 态 |
| `--glow` | `#F5C877` | **微光暖黄，全站唯一高亮** |
| `--glow-soft` | `rgba(245,200,119,.14)` | 微光卡片底色 |
| `--tp` | `#DCE0E6` | 主文字 |
| `--tb` | `#A8AEB8` | 正文 |
| `--td` | `#6B7280` | 弱化文字、标签、元信息 |
| `--done` | `#7FD1A6` | 完成 / 成功状态（低饱和） |
| `--line` | `rgba(255,255,255,.065)` | 分隔线、边框 |

### 3.2 明令禁止

- ❌ `#000000` 纯黑背景 — 最高优先级的 AI 味来源
- ❌ `#FFFFFF` 纯白文字 — 用 `--tp` `#DCE0E6`
- ❌ 紫色 / 蓝紫渐变 — agent 类产品的最大 slop，一眼假
- ❌ 饱和青色 `#00FFFF` 系做主色
- ❌ 终端绿 `#4ADE80` 用作**任何非代码场景**的主色（仅允许在代码块内）
- ❌ 同时出现 3 个以上高饱和色

### 3.3 亮色例外（极少）

仅当页面必须打印或嵌入第三方亮底环境时，可使用：
`--bg #FDFBF6`（暖米）、`--tp #0D1225`（墨黑）。
**但首页、下载页、功能页一律用暗色。**

---

## 4. 微光纪律（本文件最重要的一节）

`#F5C877` 在整站只允许出现在以下位置，**超出即违规**：

1. Logo 旁那个 6px 圆点（带光晕）
2. 标题 slogan 中的「微光」二字
3. 主行动按钮（Download）底色
4. 任务完成卡片（finish card）
5. hero 背景那一束 radial-gradient 微光

**不允许**：图标着色、边框描边、hover 态大面积染色、背景纹理、多个元素同时高亮。

```css
/* ✅ 正确：单点微光 */
.btn { background: var(--glow); }

/* ❌ 错误：微光泛滥 */
.card { border: 1px solid var(--glow); }
.icon { color: var(--glow); }
h3 span { color: var(--glow); }
```

背景微光的唯一写法（全站仅此一处）：

```css
body::before{
  content:""; position:fixed; top:-320px; left:50%;
  transform:translateX(-50%); width:1100px; height:820px;
  pointer-events:none; z-index:0;
  background:radial-gradient(ellipse at center,
    rgba(245,200,119,.085) 0%,
    rgba(245,200,119,.028) 34%,
    transparent 68%);
  animation:breathe 7s ease-in-out infinite;
}
```

---

## 5. 字体

### 5.1 三层字体，各司其职，禁止混用

| 层 | 字体 | 用途 |
|---|---|---|
| 标题（中文） | `Noto Serif SC` / `Source Han Serif SC` / `Songti SC` | 人味 + 可信 |
| 正文 | `Inter` / `Source Han Sans SC` / `Microsoft YaHei` | 中性、可靠 |
| 代码 · 终端 · 数字 | `JetBrains Mono` / `Cascadia Code` | 证据感 |

```css
--serif:"Noto Serif SC","Source Han Serif SC","Songti SC","SimSun",serif;
--sans:"Inter","Source Han Sans SC","Microsoft YaHei",-apple-system,sans-serif;
--mono:"JetBrains Mono","Cascadia Code","Consolas",monospace;
```

### 5.2 规则

- **中文标题必须用宋体（衬线）** — 这是"人味"和"可信"的来源，也是对抗 AI 味的最强单点武器
- 正文用无衬线，行高 **1.72–1.85**（中文必须比英文高）
- 所有**数字和统计值**用 mono — 让数字长得像"可查证的记录"
- 字重只使用 `400 / 500 / 600`，**禁止 700/800**（暗底上粗体显脏）
- 中文标题 `letter-spacing: .01–.06em`（宋体中文需要正字距）
- mono 小字 `letter-spacing: .14–.16em`

### 5.3 字号阶梯

```
eyebrow / label   11.5–12px   mono, --td, uppercase
正文              15px        --tb
lede              16.5px      --tb
卡片标题 h3        18px        serif 600
区块标题           26px        serif 600
hero h1           clamp(38px, 5.4vw, 60px)  serif 600, line-height 1.42
统计数字           27px        mono
```

---

## 6. 间距与节奏

### 6.1 基准 4px，但**刻意不均匀**

```
8 · 12 · 20 · 32 · 56 · 96 · 152
```

**禁止**：所有间距都是 16 / 24 / 32 这类均匀倍数。均匀间距 = 模板感。

### 6.2 区块节奏

| 区块 | padding |
|---|---|
| hero 上 | `152px` |
| hero 下 | `96px` |
| 内容区块 | `96px 0` |
| 大区块间 | `120–152px` |
| 卡片内 | `28px 26px 30px`（左右略小于上下） |
| 移动端 hero | `96px 0 64px` |

### 6.3 文字测量

- 正文行宽 **≤ 54ch**（约 54 个中文字符）
- `.lede` 用 `max-width: 54ch`
- 区块副标题用 `max-width: 48ch`

---

## 7. 圆角

**故意不统一**，这是对抗"卡片农场"的关键：

| 元素 | 圆角 |
|---|---|
| 卡片 | `14px` |
| 按钮 | `8px` |
| 代码块 / 终端 | `6px` |
| 标签 pill | `999px` |

禁止所有元素统一圆角。

---

## 8. 动效

### 8.1 时长与曲线

```
微交互（hover/color）   .2–.3s   ease
位移（translateY）      .3s      ease
呼吸（背景微光）        7s       ease-in-out infinite
```

**禁止**：弹跳曲线、超过 .4s 的过渡、超过 3 个不同的动画同时存在、滚动触发的花哨入场。

### 8.2 允许的三类动效

1. 背景微光的 `breathe`（7s 呼吸）
2. 链接 / 按钮 hover 的颜色与 `translateY(-1px)`
3. 卡片 hover 的 `border-color` 微亮 + `translateY(-2px)`

```css
@media (prefers-reduced-motion: reduce){
  *{animation:none !important; transition:none !important}
}
```

---

## 9. 版式签名（四个必用形态）

这四个形态是 Gleam 区别于所有 agent 官网的视觉签名。**新页面至少使用其中两个。**

### 签名一：双色断句标题

把一句话拆两行，前半句压暗、后半句提亮，视觉上演"交出去 → 拿回来"。

```html
<h1>
  <span class="line give">把想法交给<span class="brand">微光</span>，</span>
  <span class="line keep">把时间留给重要的事。</span>
</h1>
```
```css
h1 .give  { color: var(--tb); }
h1 .brand { color: var(--glow); position: relative; }
h1 .brand::after{
  content:""; position:absolute; left:0; right:0; bottom:-6px; height:1px;
  background:linear-gradient(90deg, var(--glow), transparent); opacity:.5;
}
h1 .keep  { color: var(--tp); }
```

可复用在：功能页标题、文档页头、README 顶部、发布公告。

---

### 签名二：推进轨（替代终端框）

**不要再用终端窗口截图讲产品。**"靠谱"讲的是"你不用管了"，不是"看我多能干"。

用一条纵向细线串起带 ✓ 的步骤，末端接一张微光完成卡片。

```html
<div class="flow">
  <div class="flow-cmd"><span class="prompt">$</span>gleam goal <span class="arg">"整理我的 markdown 文件"</span></div>
  <ul class="steps">
    <li><span class="ok">✓</span><span class="num">01</span>扫描了 23 个文件</li>
    <li><span class="ok">✓</span><span class="num">02</span>按主题归为 5 类</li>
    <li><span class="ok">✓</span><span class="num">03</span>移动 18 个文件，更新 3 处链接</li>
    <li><span class="ok">✓</span><span class="num">04</span>摘要写入 INDEX.md</li>
  </ul>
  <div class="finish">
    <div class="finish-head">
      <span class="label">完成了</span>
      <span class="score">自检得分 <b>94 / 100</b></span>
    </div>
    <p>它在你没看着的时候做完了整件事。</p>
  </div>
</div>
```

```css
.steps li{ position:relative; padding:0 0 20px 30px; font-family:var(--mono); font-size:13.5px; }
.steps li::before{ content:""; position:absolute; left:0; top:7px; width:9px; height:9px;
  border-radius:50%; background:var(--done); }
.steps li::after{ content:""; position:absolute; left:4px; top:20px; bottom:-2px; width:1px;
  background:linear-gradient(180deg, rgba(127,209,166,.32), rgba(127,209,166,.06)); }
.steps li:last-child::after{ display:none }
.finish{ margin-top:26px; padding:20px 24px; background:var(--glow-soft);
  border:1px solid rgba(245,200,119,.2); border-radius:12px; }
```

---

### 签名三：事实条（替代四宫格）

数字不用等宽 grid 装，改成一条 inline 事实条，细线分隔。

```css
.facts{ display:flex; border-top:1px solid var(--line);
  border-bottom:1px solid var(--line); padding:30px 0; }
.fact{ padding:0 44px; border-left:1px solid var(--line); }
.fact:first-child{ padding-left:0; border-left:none }
.fact .n{ font-family:var(--mono); font-size:27px; color:var(--tp); }
.fact .n small{ font-size:14px; color:var(--td); }
.fact .k{ font-size:12.5px; color:var(--td); margin-top:5px; }
```

---

### 签名四：错落栏（禁止等宽 grid）

```css
.ev-grid{ display:grid; grid-template-columns:1.4fr 1fr 1.1fr; gap:20px; }
```

**禁止** `repeat(3, 1fr)` 或 `repeat(auto-fit, minmax(280px,1fr))` 产生的等宽三栏。
对称本身就在喊"我是模板"。

---

## 10. 组件规范

### 按钮

```css
.btn{ display:inline-flex; align-items:center; gap:10px;
  background:var(--glow); color:#14171D; font-weight:600; font-size:14.5px;
  padding:13px 26px; border-radius:8px; }
.btn:hover{ transform:translateY(-1px);
  box-shadow:0 8px 26px -6px rgba(245,200,119,.5); }

.btn-ghost{ color:var(--tb); font-size:14.5px;
  border-bottom:1px solid var(--line); padding-bottom:3px; }
.btn-ghost:hover{ color:var(--tp); border-color:var(--tb); }
```

- 主按钮**每屏最多一个**
- 次要行动一律用 `btn-ghost`（下划线式，不是描边按钮）
- **禁止**：主按钮用描边样式、一屏两个实心主按钮

### 卡片

```css
.card{ background:var(--raised); border:1px solid var(--line);
  border-radius:14px; padding:28px 26px 30px; }
.card:hover{ border-color:rgba(245,200,119,.22); transform:translateY(-2px); }
```

- 默认**无阴影**，靠 1px hairline 分隔
- 禁止 `box-shadow` 做常态抬升
- 禁止每张卡片都配一个图标

### 标签

```css
.tag{ font-family:var(--mono); font-size:11px; color:var(--td);
  border:1px solid var(--line); padding:4px 11px; border-radius:999px; }
```

### 导航

- 高度靠 `padding:26px 0` 撑，不用固定 height
- 底部 1px hairline，不用阴影
- 链接默认 `--td`，hover 到 `--tp`
- 最多 4 个导航项

---

## 11. 文案规则

### 11.1 核心句式

✅ **说用户得到什么**：「把时间留给重要的事」
❌ **说产品是什么**：「Not a chatbot. A reliable colleague.」

### 11.2 禁用词库

| 禁用 | 原因 |
|---|---|
| 赋能 / 一站式 / 重塑 / 颠覆 | 中文 slop 四大件 |
| Not just X, but Y | 英文 slop 头号句式 |
| 你的 AI 同事 / 你的数字伙伴 | 已被用烂，做标题即失效 |
| 革命性的 / 前所未有的 | 无证据的形容词 |
| 智能、强大、高效（单独出现） | 必须换成数字 |

### 11.3 强制规则

- 中文内容用**中文全角标点**
- 数字与单位之间不加空格：`10 MB`、`27 个工具`
- 中英文之间加一个空格：`用 Gleam 整理`
- 能用数字就不用形容词：`94/100` 优于 `表现优异`
- 英文原文文案不要硬翻成中文，直接删掉重写
- 每段不超过 3 行

---

## 12. 反 Slop 清单

生成任何页面后，逐条勾选。**任一命中即打回重做。**

**色彩类**
- [ ] 有纯黑 `#000` 背景吗？
- [ ] 有纯白 `#FFF` 文字吗？
- [ ] 有紫色 / 蓝紫渐变吗？
- [ ] 高亮色 `#F5C877` 是否出现在 §4 允许的 5 个位置之外？
- [ ] 高亮色占比是否超过 5%？

**布局类**
- [ ] 有等宽 3 宫格 / 4 宫格 grid 吗？
- [ ] 所有间距是不是只有 3 个值在循环？
- [ ] 所有圆角是不是同一个数值？
- [ ] 是不是所有区块都居中对称？
- [ ] 是不是每张卡片都配了一个图标？

**字体类**
- [ ] 中文标题用了黑体而非宋体吗？
- [ ] 用了 700 / 800 字重吗？
- [ ] 数字用了非 mono 字体吗？

**内容类**
- [ ] 有 emoji 当图标用吗？
- [ ] 有拟人头像 / 对话气泡吗？
- [ ] 有 "Hi 我是你的 AI 同事" 这类开场吗？
- [ ] 有 terminal 窗口截图当主视觉吗？（应换成推进轨）
- [ ] 形容词是不是多于数字？

**动效类**
- [ ] 有超过 3 种动画同时存在吗？
- [ ] 有超过 .4s 的过渡吗？
- [ ] 有弹跳 / 花哨滚动入场吗？

---

## 13. 交付自检（质量封印）

AI 完成页面后，必须在回复末尾输出自检结果：

```
【设计自检】
微光使用点：__ 处（上限 5）  ✅ / ❌
等宽 grid：__ 处（应为 0）   ✅ / ❌
中文标题宋体：✅ / ❌
反 slop 清单：__ / 18 项通过
```

**任何 ❌ 必须修复后再交付，不允许带着 ❌ 交稿。**

---

## 14. 页面骨架模板

```html
<div class="wrap">
  <nav>            <!-- logo + 最多 4 个链接 -->
  <header>         <!-- eyebrow → 双色断句 h1 → lede → CTA → meta-note -->
  <section flow>   <!-- 推进轨：命令 + 步骤 + 完成卡片 -->
  <section facts>  <!-- 事实条 -->
  <section evidence> <!-- 错落栏 1.4:1:1.1 -->
  <section closing>  <!-- 收尾 CTA，双色断句 -->
  <footer>
</div>
```

```css
.wrap{ max-width:1080px; margin:0 auto; padding:0 40px; position:relative; z-index:1 }
```

**响应式断点**（`820px`）：错落栏转单列、事实条去掉竖线、hero padding 收到 `96px 0 64px`。

---

## 15. 与 taste-skill 的分工

| 维度 | 由谁决定 |
|---|---|
| 布局节奏、间距不均匀、动效克制 | taste-skill（通用审美底座） |
| 配色、字体、微光纪律、版式签名、文案 | **GLEAM-DESIGN.md（本文件）** |
| 冲突解决 | 以本文件为准 |

推荐 prompt 写法：

```
遵循 GLEAM-DESIGN.md 的身份约束（配色/字体/微光纪律/版式签名），
同时用 taste-skill 的布局与动效原则处理节奏问题。
做完输出【设计自检】。
```

---

## 变更记录

- **v0.1** (2026-10-15) 初版。确立暗底 + 单点微光 + 宋体标题 + 四个版式签名。
