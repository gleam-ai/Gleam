// 界面语言：中英切换的词表与翻译器。
//
// 覆盖范围是**整个静态界面**——顶栏、侧栏、输入区、菜单、面板坞、设置页、市场、
// 站点、记忆、成长、快捷键，以及 placeholder / title / aria-label 属性层。
// **不覆盖后端返回的运行时文字**（任务进度、通知、报错）：那些字从 Go 侧来，
// 前端拿到的就已经是成品句子，这里只负责界面自己的文案。
//
// 翻译是增量的：DOM 一变就补翻新增的部分，所以设置页、任务卡、对话气泡这些
// 后渲染的内容也会被翻到（早期版本只在加载时走一遍，切英文后打开设置页仍是中文）。
//
// 翻译单位是**整个文本节点 / 整个属性值**：键必须与界面上的原文逐字相等，
// 带插值的句子（如「共 3 个快捷键」）走下面的 I18N_PATTERNS。
const I18N_EN = {
  目标: 'Goals',
  定时任务: 'Schedules',
  技能: 'Skills',
  工具: 'Tools',
  更多: 'More',
  记忆: 'Memory',
  市场: 'Market',
  成长: 'Growth',
  设置: 'Settings',
  我的: 'Me',
  新任务: 'New task',
  '不止于对话，把事做完': 'Beyond chat, get it done',
  '上下文留在本机，Gleam 帮你一步步推进':
    'Context stays on this machine while Gleam moves the work forward',
  会话: 'Chats',
  任务: 'Tasks',
  收起侧栏: 'Collapse sidebar',
  展开侧栏: 'Expand sidebar',
  展开现场栏: 'Open live panel',
  收起现场栏: 'Close live panel',
  搜索: 'Search',
  工作区: 'Workspaces',
  新对话: 'New chat',
  选择工作区: 'Choose workspace',
  准备就绪: 'Ready',
  可以开始一个新目标: 'Start a new goal',
  全部: 'All',
  进行中: 'Active',
  已完成: 'Done',
  需处理: 'Needs you',
  本地工作台: 'Local workbench',
  '不止于对话，': 'Beyond chat, ',
  '把事做完。': 'get it done.',
  '描述一个目标，或从下方快捷入口开始。上下文留在本机，持续推进。':
    'Describe a goal or start from a shortcut below. Context stays on this machine.',
  本周完成: 'Done this week',
  条记忆: 'memories',
  整理工作区: 'Tidy workspace',
  项目复盘: 'Project review',
  快速研究: 'Quick research',
  '文件归档 · 生成索引': 'Archive files · build index',
  '进度分析 · 风险提醒': 'Progress · risks',
  '阅读文档 · 输出简报': 'Read docs · brief',
  可以直接开口说了: 'Go ahead and say it',
  对话: 'Chat',
  工作: 'Work',
  编程: 'Code',
  执行前询问: 'Ask first',
  自动执行: 'Auto-run',
  仅对话: 'Chat only',
  通用: 'General',
  执行权限: 'Access',
  任务档位: 'Task mode',
  专家角色: 'Expert role',
  演示模型: 'Demo model',
  本机: 'Local',
  '继续当前会话…': 'Continue this chat…',
  执行方式: 'Run mode',
  工作模式: 'Work mode',
  添加文件: 'Add file',
  'Enter 发送 · Shift+Enter 换行': 'Enter to send · Shift+Enter for newline',
  '一切从这里开始… 描述任务，或输入 @ 引用':
    'Start here… describe a task, or type @ to reference',
  添加文件夹: 'Add folder',
  添加目标: 'Add goal',
  计划模式: 'Plan mode',
  添加插件: 'Add plugin',
  '@ 引用': '@ mention',
  浏览器预览: 'Browser preview',
  立即压缩: 'Compress now',
  去设置查看: 'Open settings',
  当前模型: 'Current model',
  长期记忆: 'Long-term memory',
  写入记忆: 'Save a memory',
  保存: 'Save',
  检索你的记忆: 'Search your memory',
  写一条记忆: 'Write a memory',
  新建定时任务: 'New schedule',
  创建: 'Create',
  从市场安装: 'Install from market',
  'MCP 服务器': 'MCP servers',
  技能模板: 'Skill templates',
  已安装: 'Installed',
  自定义接入: 'Custom',
  个人: 'Personal',
  智能体: 'Agent',
  安全: 'Safety',
  协作: 'Collaboration',
  外观: 'Appearance',
  模型: 'Model',
  引擎: 'Engine',
  'Go 工具链': 'Go toolchain',
  账号与登录: 'Account',
  累计任务: 'Tasks',
  连续成功: 'Streak',
  '本周 tokens': 'Tokens this week',
  语言: 'Language',
  明暗模式: 'Mode',
  主题: 'Theme',
  字体风格: 'Font',
  文字大小: 'Text size',
  界面缩放: 'Zoom',
  内容宽度: 'Content width',
  系统: 'System',
  浅色: 'Light',
  深色: 'Dark',
  跟随系统: 'System',
  微光: 'Gleam',
  站点: 'Sites',
  我的站点: 'My sites',
  共享给我的: 'Shared with me',
  添加站点: 'Add site',
  还没有站点: 'No sites yet',
  落地页: 'Landing',
  作品集: 'Portfolio',
  博客与内容: 'Blog',
  数据看板: 'Dashboard',
  内部工具: 'Internal tool',
  其他: 'Other',
  森林: 'Forest',
  薄荷: 'Mint',
  蜜蜂: 'Bee',
  羊皮纸: 'Parchment',
  无衬线: 'Sans',
  衬线: 'Serif',
  小: 'S',
  中: 'M',
  大: 'L',
  标准: 'Standard',
  宽: 'Wide',
  使用统计与成长: 'Usage & growth',
  检查更新: 'Check for updates',
  帮助: 'Help',
  账号与本地数据: 'Account & local data',
  退出登录: 'Sign out',
  所有任务: 'All tasks',
  选择: 'Select',
  打开: 'Open',
  个: '',
  '搜索会话标题、内容摘要或目标…': 'Search chats, previews or goals…',
  '搜索设置…': 'Search settings…',
  现场: 'Live',
  空闲: 'Idle',
  外观皮肤: 'Appearance',
  深浅模式: 'Mode',
  强调色: 'Accent',
  '0 = 不检测': '0 = off',
  '0 = 不限': '0 = unlimited',
  '0 = 全量注入': '0 = inject everything',
  '127.0.0.1:5173 或完整地址': '127.0.0.1:5173 or a full URL',
  '3600（即每小时）': '3600 (i.e. hourly)',
  'API Key（绑定当前 API 地址的主机，换厂商需重填）':
    'API key (bound to the host of the current API URL; re-enter when switching providers)',
  'API 地址（留空回落厂商官方入口）':
    "API base URL (leave empty to use the provider's official endpoint)",
  'GEO 准则': 'GEO rules',
  'GEO 生成式引擎优化': 'GEO — generative engine optimization',
  'Gleam · 微光': 'Gleam',
  'Go 已检测到': 'Go detected',
  'OpenAI Chat 兼容（GLM/DeepSeek/Kimi 等）':
    'OpenAI Chat compatible (GLM / DeepSeek / Kimi, etc.)',
  'Supabase 项目地址': 'Supabase project URL',
  'Supabase 项目地址 https://xxxx.supabase.co':
    'Supabase project URL https://xxxx.supabase.co',
  'token 上限': 'Token cap',
  上一个: 'Previous',
  '上一个（Shift+Enter）': 'Previous (Shift+Enter)',
  上一步: 'Back',
  上一页: 'Previous page',
  '上一页（只走我们自己的地址栈，跨站页面里我们控制不了它的历史）':
    "Previous page (only walks our own address stack; we can't control history inside cross-site pages)",
  上下文窗口: 'Context window',
  '上面输一个本机地址，或点上面常用端口。':
    'Type a local address above, or pick a common port.',
  下一个: 'Next',
  '下一个（Enter）': 'Next (Enter)',
  下一步: 'Next',
  下一页: 'Next page',
  '不再询问，可自由访问你的文件、终端和网络':
    "Don't ask again — free access to your files, terminal and network",
  不指定工作区: 'No workspace',
  '不限定领域，适合日常办公与杂项任务':
    'No fixed domain; good for everyday office and misc tasks',
  严谨: 'Precise',
  个人资料: 'Profile',
  '为选定的厂商填入 API Key，我们会测试连通性。':
    "Enter an API key for the selected provider; we'll test connectivity.",
  事件流水: 'Event stream',
  云端已登录: 'Signed in to cloud',
  仅在检测到潜在风险时询问: 'Ask only when a potential risk is detected',
  仅高风险操作需要批准: 'Only high-risk actions need approval',
  从工作区选择: 'Choose from workspace',
  '从当前工作区选择文件，引用到输入框。':
    'Pick files from the current workspace and reference them in the input.',
  从本地选择: 'Choose from local',
  从本机导入记忆: 'Import memory from this machine',
  '代码编写、调试、重构与技术架构':
    'Writing, debugging, refactoring and technical architecture',
  以打开终端: 'to open the terminal',
  任务名: 'Task name',
  任务在这台电脑上执行: 'Runs on this computer',
  任务工作台状态: 'Workbench status',
  '任务时长上限（秒）': 'Max task duration (s)',
  任务模式: 'Task mode',
  任务监控: 'Task monitor',
  任务输入: 'Task input',
  任务预算熔断: 'Task budget circuit breaker',
  会话上下文自动压缩: 'Auto-compress conversation context',
  低价值调用去重: 'Deduplicate low-value calls',
  '你是做决定的那个人，具体执行交给我。':
    'You make the calls; I handle the execution.',
  使用统计: 'Usage',
  '例如 32000': 'e.g. 32000',
  '例如：产品发布公众号推文': 'e.g. product launch post',
  '例如：我喜欢简洁的回复': 'e.g. I prefer concise replies',
  '例如：每天早上9点 / 工作日18:30 / 每周一和周五晚8点 / 每月1号9点 / 每隔30分钟':
    'e.g. every day at 9am / weekdays at 18:30 / Mon and Fri at 8pm / the 1st at 9am / every 30 minutes',
  侧栏: 'Sidebar',
  侧栏入口: 'Sidebar entries',
  保存连接: 'Save connection',
  信任级别: 'Trust level',
  '信息检索、竞品分析、行业调研与知识整理':
    'Information retrieval, competitor analysis, industry research and knowledge organization',
  '修改「切换任务」的快捷键': 'Change the shortcut for "Switch task"',
  '修改「切换工作模式」的快捷键': 'Change the shortcut for "Switch work mode"',
  '修改「前进」的快捷键': 'Change the shortcut for "Forward"',
  '修改「打开内置浏览器」的快捷键':
    'Change the shortcut for "Open built-in browser"',
  '修改「打开工作区文件」的快捷键':
    'Change the shortcut for "Open workspace files"',
  '修改「打开或关闭右侧栏」的快捷键':
    'Change the shortcut for "Open or close right panel"',
  '修改「打开或关闭左侧栏」的快捷键':
    'Change the shortcut for "Open or close left panel"',
  '修改「打开或关闭终端面板」的快捷键':
    'Change the shortcut for "Open or close terminal panel"',
  '修改「打开终端」的快捷键': 'Change the shortcut for "Open terminal"',
  '修改「搜索当前任务」的快捷键':
    'Change the shortcut for "Search in this task"',
  '修改「新任务」的快捷键': 'Change the shortcut for "New task"',
  '修改「新建窗口」的快捷键': 'Change the shortcut for "New window"',
  '修改「设置」的快捷键': 'Change the shortcut for "Settings"',
  '修改「返回」的快捷键': 'Change the shortcut for "Back"',
  修改快捷键: 'Change shortcut',
  候补目标: 'Follow-up goals',
  做一个能直接打开的网页: 'Make a web page you can open right away',
  '先花一分钟接入一个模型，之后的一切就开始了。':
    'Connect a model first — that takes a minute, and everything starts from there.',
  先选择一个工作区: 'Choose a workspace first',
  全选: 'Select all',
  全部设置: 'All settings',
  关闭: 'Close',
  关闭查找: 'Close find',
  关闭终端面板: 'Close terminal panel',
  关闭面板: 'Close panel',
  '关闭面板（Esc）': 'Close panel (Esc)',
  '关闭（Esc）': 'Close (Esc)',
  内容创作者: 'Content creator',
  '内容标题 / 目标（可选）': 'Content title / goal (optional)',
  '内置档位名：economy（通用/低难度）、coding（开发·运维）、office（写作·项目）、reasoning（分析·调研）。档位没配或角色没声明，一律用主模型。':
    "Built-in tier names: economy (general / easy), coding (dev & ops), office (writing & projects), reasoning (analysis & research). If a tier is unset or the role doesn't declare one, the main model is used.",
  分析: 'Analyze',
  分析会调用一次模型: 'Analysis makes one model call',
  分析历史: 'Analysis history',
  '切换「请我批准」': 'Toggle "Ask me first"',
  切换任务: 'Switch task',
  '切换到其他状态继续查看。': 'Switch to another status to keep looking.',
  '切换右侧入口栏（工作区文件 / 内置浏览器 / 终端）。':
    'Toggle the right rail (workspace files / built-in browser / terminal).',
  切换工作模式: 'Switch work mode',
  '切换左侧导航栏的展开状态。': 'Toggle the left navigation sidebar.',
  '切换底部终端面板。': 'Toggle the bottom terminal panel.',
  切换模型: 'Switch model',
  列表: 'List',
  创作任务完成后自动分析产出并给出建议:
    'Analyze output and suggest improvements after a creative task',
  创作后自动分析: 'Auto-analyze after creation',
  创作时自动注入给模型: 'Injected into the model when creating',
  '到点自动执行目标。直接用中文描述“每天几点、工作日、每周几、每隔多久”就行。':
    'Goals run automatically when due. Just describe the timing in plain words — "every day at 9", "weekdays at 18:30", "every 30 minutes".',
  到点要做什么: 'What should happen when due',
  刷新: 'Refresh',
  '刷新（重新载入这一个地址）': 'Refresh (reload this address)',
  前进: 'Forward',
  '前进（Ctrl+]）': 'Forward (Ctrl+])',
  剪切: 'Cut',
  '加载中…': 'Loading…',
  助手名称: 'Assistant name',
  助手名称与协作风格: 'Assistant name and tone',
  协作风格: 'Tone',
  '协作风格、安全策略、引擎与上下文压缩。保存后立即生效，重启后依然保留。':
    'Tone, safety policy, engine and context compression. Changes apply immediately and survive restarts.',
  '单个任务超过预算会立即停下并征求你的确认；填 0 表示不限制该项。':
    'A task that exceeds its budget stops immediately and asks you; 0 means no limit on that item.',
  单次规划工具上限: 'Tool cap per plan',
  '单步超时（秒）': 'Step timeout (s)',
  '占用是「最近一次请求的输入 ÷ 模型窗口」。溢出窗口的旧对话随时汇总成摘要，不会丢；占用到 90%（任务已结束）或 96%（任务进行中）时会自动收紧下一轮携带的对话，压完接着把任务做完。':
    'Usage is "the last request\'s input tokens ÷ the model window". Conversations that spill outside the window are summarized at any time, never dropped; at 90% (task finished) or 96% (task still running) the next round carries fewer turns automatically, and the task continues afterwards.',
  '厂商、套餐、协议与密钥': 'Provider, plan, protocol and key',
  厂商预设: 'Provider presets',
  '参数（空格分隔）': 'Arguments (space-separated)',
  发送: 'Send',
  发送消息: 'Send message',
  发送键: 'Send key',
  '发送（Enter）': 'Send (Enter)',
  '只保存项目地址与公开 anon key（非机密），用于建立登录。你的业务数据仍全部在本地。':
    'Only the project URL and the public anon key (not a secret) are stored, to establish sign-in. Your business data stays entirely local.',
  只读放行: 'Read-only allowed',
  '可用中文、字母、数字与 - . _ 空格；不能含 / （它是任务的寻址标识，带斜杠将无法暂停或删除）':
    'Letters, digits, spaces and - . _ ; a / is not allowed (it addresses the task, and a slash makes it impossible to pause or delete)',
  可选模型: 'Optional model',
  右侧栏: 'Right panel',
  同一动作重复这么多轮就停下: 'Stop after this many repeats of the same action',
  '名称（可选，默认取命令名；限字母数字与 - _，≤32 字符）':
    'Name (optional; defaults to the command name; letters, digits, - _ only, ≤32 chars)',
  '周一、周五 20:00': 'Mon, Fri 20:00',
  命令: 'Command',
  '哪些操作被拦下、哪些被放行、哪些是审核模型加拦的——留痕才能复盘。':
    'Which actions were blocked, which were allowed, and which the review model flagged — a trail is what makes review possible.',
  固化技能: 'Save as skill',
  '在任务输入框按 Enter 发送，Shift+Enter 换行。':
    'Press Enter in the task input to send; Shift+Enter for a newline.',
  '在右上角输入关键词回车，或先写下一条你希望 Gleam 记住的事。':
    'Type a keyword and press Enter at the top right, or write down something you want Gleam to remember.',
  在当前任务中查找: 'Find in this task',
  '在当前可见的任务内容中查找文字。':
    'Find text within the visible task content.',
  在当前页面查找: 'Find on this page',
  在此空间新建对话: 'New chat in this space',
  '在浏览器里，Ctrl+N / Ctrl+T / Ctrl+Shift+N 这类组合键会先被浏览器拦下；换一个组合，或使用桌面版。':
    'In a browser, shortcuts like Ctrl+N / Ctrl+T / Ctrl+Shift+N are captured by the browser first; pick another combination, or use the desktop app.',
  在系统浏览器打开: 'Open in system browser',
  '在系统浏览器打开（Google、GitHub 这类站点拒绝被嵌入，走这条）':
    'Open in system browser (sites like Google and GitHub refuse embedding — use this)',
  '在编程模式和通用模式之间切换。': 'Switch between Code and General mode.',
  '在页面导航历史中前进到下一页。': 'Go forward in the page history.',
  '在页面导航历史中返回上一页。': 'Go back in the page history.',
  地址: 'Address',
  '填写 API Key': 'Enter API key',
  复制: 'Copy',
  复盘: 'Review',
  '外观偏好只保存在本机，不影响你的任何数据。':
    "Appearance preferences are stored locally only and don't touch your data.",
  套餐类型: 'Plan type',
  '如果 Gleam 未检测到 Go，请填写 Go 安装路径（包含 bin 子目录的文件夹）':
    "If Gleam can't find Go, enter its install path (the folder containing the bin subdirectory)",
  '如：文件 / fetch / git，回车搜索':
    'e.g. file / fetch / git, Enter to search',
  '如：速记 / 日志 / 网页，回车搜索':
    'e.g. notes / logs / web, Enter to search',
  安全门控: 'Safety gate',
  安装: 'Install',
  完全访问: 'Full access',
  完成: 'Done',
  完成任务: 'Finish task',
  完成度达标线: 'Completion threshold',
  '定时任务由本机的 Gleam 按分钟心跳触发；Gleam 关闭期间不会执行，重新打开后自动恢复并重算下次时间。':
    'Schedules are triggered by a per-minute heartbeat from Gleam on this machine; nothing runs while Gleam is closed, and it resumes and recomputes the next run on reopen.',
  实验功能: 'Experimental features',
  '审批超时（秒）': 'Approval timeout (s)',
  审阅: 'Review',
  密码: 'Password',
  '密码（至少 6 位）': 'Password (at least 6 characters)',
  '密钥、对话、技能、MCP 都只存在这台电脑':
    'Keys, chats, skills and MCP all stay on this computer',
  '对本来会自动放行的中高风险动作先审一遍，被标记才交给你确认。需要配置「辅助模型」才生效':
    'Pre-screen medium- and high-risk actions that would otherwise be auto-allowed; only flagged ones come to you. Requires a helper model.',
  对话模式回答后独立自检: 'Self-check chat answers after replying',
  '对话模式没有规划阶段，原本无条件报满分。开启后由辅助模型核对回答是否达成目标，只过一半就如实判「部分完成」。需要配置「辅助模型」才生效':
    'Chat mode has no planning phase, so it used to always score full marks. When enabled, a helper model checks whether the answer met the goal and honestly reports "partially complete" if it only got halfway. Requires a helper model.',
  导入选中: 'Import selected',
  导航: 'Navigation',
  '尚无记录，完成第一个任务即可开始成长。':
    'No records yet — finish your first task to start growing.',
  工作区索引: 'Workspace index',
  工作台概览: 'Workbench overview',
  '工作日 18:30': 'Weekdays 18:30',
  工作统计: 'Work stats',
  '工具只读，自动放行': 'Read-only tool, allowed automatically',
  '工具输出预算（字）': 'Tool output budget (chars)',
  已停用: 'Disabled',
  已启用: 'Enabled',
  已归档: 'Archived',
  已按下: 'Pressed',
  市场分区: 'Market sections',
  常用地址: 'Common addresses',
  常用时间: 'Common times',
  '常见 MCP 服务器与技能模板，一键安装，安装即热生效；也支持自定义 stdio 接入。':
    'Common MCP servers and skill templates — install in one click, effective immediately; custom stdio servers are supported too.',
  常规: 'Standard',
  平均分: 'Average score',
  平均完成度: 'Average completion',
  '平实友好，像可靠的同事': 'Plain and friendly, like a reliable colleague',
  应用快捷键: 'App shortcuts',
  应用菜单: 'App menu',
  开发工程师: 'Software engineer',
  '开始新对话：清空当前对话上下文（长期记忆保留）':
    'Start a new chat: clears the current conversation context (long-term memory is kept)',
  开始配置: 'Start setup',
  引擎事件流水: 'Engine event stream',
  '引擎参数、调用治理与预算策略':
    'Engine parameters, call governance and budget policy',
  '引擎注册的全部能力。非只读工具调用时同样需要你的批准。':
    'Every capability the engine has registered. Calls to non-read-only tools also need your approval.',
  引用历史任务: 'Reference a past goal',
  '引用已安装 MCP 或技能': 'Reference an installed MCP or skill',
  引用建议: 'Reference suggestions',
  归档管理: 'Archive',
  '当前模型，点击切换': 'Current model — click to switch',
  当前等级: 'Current level',
  待分析内容: 'Content to analyze',
  待批准: 'Awaiting approval',
  '很多站点（Google、GitHub、需要登录的页面）拒绝被嵌在这里，一直空白就是被拒了——用「在系统浏览器打开」。':
    'Many sites (Google, GitHub, pages that require sign-in) refuse to be embedded here; a permanently blank pane means they refused — use "Open in system browser".',
  循环与上下文治理: 'Loop and context governance',
  微光循环: 'Gleam loop',
  微光空间与会话: 'Spaces and chats',
  快捷任务: 'Quick tasks',
  快捷键: 'Shortcuts',
  总执行时长: 'Total run time',
  '想用「每隔一段时间」自动跑？': 'Want it to run every so often?',
  成长指标: 'Growth metrics',
  成长足迹: 'Growth trail',
  或使用: 'or use',
  手动分析: 'Analyze manually',
  '手动指定 Go 安装路径（bin 的父目录）':
    'Specify the Go install path manually (the parent of bin)',
  '手输 / 改厂商': 'Enter / change provider manually',
  '打开 Gleam 设置。': 'Open Gleam settings.',
  '打开一个新的 Gleam 窗口。': 'Open a new Gleam window.',
  打开内置浏览器: 'Open built-in browser',
  打开工作区文件: 'Open workspace files',
  '打开底部终端面板并聚焦命令行。':
    'Open the bottom terminal panel and focus the command line.',
  打开或关闭右侧栏: 'Open or close the right panel',
  打开或关闭左侧栏: 'Open or close the left panel',
  打开或关闭终端面板: 'Open or close the terminal panel',
  '打开新的任务输入页。': 'Open a new task input page.',
  '打开浏览器预览面板，查看本机服务。':
    'Open the browser preview panel to view local services.',
  打开终端: 'Open terminal',
  打开终端面板: 'Open terminal panel',
  '打转判定时长（轮）': 'Loop-detection window (turns)',
  执行: 'Execute',
  执行前用辅助模型快筛: 'Pre-screen with a helper model before running',
  执行前确认完整计划: 'Confirm the full plan before running',
  '执行命令、修改 Workspace 外文件或访问网络前，始终询问':
    'Always ask before running commands, editing files outside the workspace, or accessing the network',
  '执行时间（用话说就行）': 'Timing (plain words are fine)',
  执行模式: 'Run mode',
  '执行模式、审批超时与审核策略':
    'Run mode, approval timeout and review policy',
  扩展管理: 'Extensions',
  扫描本机: 'Scan this machine',
  技能失败自动优化: 'Auto-optimize failed skills',
  把想法做成一张能直接打开的网页:
    'Turn an idea into a web page you can open right away',
  '把成功的执行流程固化为可复用技能，一键重放。':
    'Save a successful run as a reusable skill and replay it in one click.',
  拆分面板: 'Split panel',
  拉取厂商模型: 'Fetch provider models',
  拉取模型: 'Fetch models',
  拖动调整终端高度: 'Drag to resize the terminal',
  拖动调整输入框高度: 'Drag to resize the input box',
  拖动调整面板宽度: 'Drag to resize the panel',
  拖动调整侧栏宽度: 'Drag to resize the sidebar',
  按下次执行: 'By next run',
  '按住上下拖动，调整输入框高度（双击恢复自动高度）':
    'Drag up or down to resize the input box (double-click to restore auto height)',
  按分类筛选技能: 'Filter skills by category',
  按创建顺序: 'By creation order',
  按名称: 'By name',
  '按字面与词组匹配，不认同义词':
    'Matches literally and by phrase, no synonyms',
  按标签筛选: 'Filter by tag',
  按状态筛选: 'Filter by status',
  排列方式: 'Layout',
  排序: 'Sort',
  探索: 'Explore',
  接入主机: 'Endpoint host',
  '描述精确，不夸大': 'Precise wording, no exaggeration',
  '搜索 MCP 服务器': 'Search MCP servers',
  '搜索、查看并修改 Gleam 内置命令的快捷键。修改后会立即保存在当前设备。':
    "Search, view and change shortcuts for Gleam's built-in commands. Changes are saved on this device immediately.",
  搜索任务: 'Search tasks',
  '搜索任务（Ctrl+K）': 'Search tasks (Ctrl+K)',
  '搜索命令、说明或组合键': 'Search commands, descriptions or key combinations',
  '搜索并切换到另一个任务。': 'Find and switch to another task.',
  搜索当前任务: 'Search in this task',
  搜索快捷键: 'Search shortcuts',
  搜索技能模板: 'Search skill templates',
  搜索结果: 'Search results',
  搜索设置: 'Search settings',
  撤销: 'Undo',
  '数据分析、表格处理、统计与可视化':
    'Data analysis, spreadsheets, statistics and visualization',
  数据分析师: 'Data analyst',
  整理下载目录: 'Tidy the downloads folder',
  文件: 'File',
  '文件 · 目标 · 插件 · 记忆': 'Files · Goals · Plugins · Memory',
  '文案撰写、报告生成、内容润色与翻译':
    'Copywriting, report writing, editing and translation',
  新建浏览器标签: 'New browser tab',
  新建窗口: 'New window',
  星云紫: 'Nebula purple',
  显示侧边栏: 'Show sidebar',
  晴空蓝: 'Sky blue',
  暂无对话: 'No chats yet',
  暖阳橙: 'Sunset orange',
  '最大 tokens': 'Max tokens',
  最大化: 'Maximize',
  最大化面板: 'Maximize panel',
  最小化: 'Minimize',
  最高分: 'Best score',
  未设置: 'Not set',
  本地数据: 'Local data',
  本地模式: 'Local mode',
  '本地模式 · 功能完整': 'Local mode · fully featured',
  '本地模式 · 数据只在这台电脑': 'Local mode · data stays on this computer',
  本机检测: 'Local check',
  本机活动: 'Local activity',
  权限模式: 'Permission mode',
  '来源: 系统 PATH · C:\\Program Files\\Go':
    'Source: system PATH · C:\\Program Files\\Go',
  核心功能: 'Core features',
  检测: 'Check',
  检索记忆: 'Search memory',
  '检索记忆，回车搜索': 'Search memory, Enter to search',
  概览: 'Overview',
  '模型档位（可选，一行一条「档位: 模型」，供专家角色按场景挑模型）':
    'Model tiers (optional; one "tier: model" per line, so expert roles can pick a model per scenario)',
  模型调用次数上限: 'Cap on model calls',
  '模型（支持 Coding / Agent / Token 套餐与自定义协议）':
    'Model (supports Coding / Agent / Token plans and custom protocols)',
  模式配置: 'Mode configuration',
  樱粉: 'Cherry pink',
  '欢迎使用 Gleam 微光': 'Welcome to Gleam',
  步骤并发上限: 'Step concurrency cap',
  每个写操作逐步确认: 'Confirm every write step',
  '每周一 9:00': 'Mondays 9:00',
  '每天 9:00': 'Daily 9:00',
  每日整理: 'Daily tidy',
  '每月1号 9:00': 'The 1st of each month at 9:00',
  每次调用需要你批准: 'Each call needs your approval',
  每步批准: 'Approve each step',
  '每隔 30 分钟': 'Every 30 minutes',
  '每隔多少秒跑一次（留空则按上面的说话时间）':
    'Run every N seconds (leave empty to use the phrasing above)',
  没有任务在跑: 'No task running',
  没有匹配的设置: 'No matching settings',
  '注入下游前按「头+尾」截断': 'Truncate head + tail before passing downstream',
  注册: 'Sign up',
  测试连接: 'Test connection',
  '测试连接按表单当前值发起一次最小请求（不必先保存）；密钥保存在本地加密存储，不会回传页面。':
    "Test connection sends one minimal request using the form's current values (no need to save first); keys live in local encrypted storage and are never sent back to the page.",
  浏览器: 'Browser',
  '浏览器会先拦下这个组合键；在桌面版里生效':
    'The browser intercepts this shortcut first; it works in the desktop app',
  浏览器内被占用: 'In use inside the browser',
  浏览器标签: 'Browser tabs',
  添加上下文: 'Add context',
  添加附件: 'Add attachment',
  清空: 'Clear',
  清空摘要: 'Clear summary',
  清空输出: 'Clear output',
  清除密钥: 'Clear key',
  温和: 'Gentle',
  温度: 'Temperature',
  版本: 'Version',
  '现场 · LIVE': 'Live · LIVE',
  电脑操控: 'Computer control',
  '留空表示都用上面的主模型，例如填一个更便宜更快的模型':
    'Leave empty to use the main model above; set a cheaper, faster model if you like',
  登录: 'Sign in',
  '直击要点，不做冗余解释': 'Gets to the point, no filler',
  '直聊问答，不经规划执行': 'Direct Q&A, no planning or execution',
  '相同参数的只读调用只跑一次，已知失败的重复调用直接跳过':
    'Identical read-only calls run once; repeats of known failures are skipped',
  看本机跑起来的样子: 'See what it looks like running locally',
  '短期窗口（轮）': 'Short-term window (turns)',
  研究员: 'Researcher',
  '离线演示模型：不发起真实网络调用':
    'Offline demo model: makes no real network calls',
  '窗口外旧对话自动摘要，省 token 不丢上下文':
    'Old turns outside the window are summarized automatically — saves tokens without losing context',
  站点分区: 'Site sections',
  站点模板: 'Site templates',
  筛选任务: 'Filter tasks',
  简体中文: 'Simplified Chinese',
  粘贴: 'Paste',
  '粘贴一段文案、报告或文章，分析它在 AI 问答引擎中的可引用性':
    'Paste a piece of copy, a report or an article to see how citable it is to AI answer engines',
  '系统运维、脚本自动化、日志分析与故障排查':
    'System operations, scripting, log analysis and troubleshooting',
  '累计 token': 'Total tokens',
  累计分析: 'Total analyses',
  线协议: 'Wire protocol',
  终端: 'Terminal',
  '编程准则：最小 diff + 验证步骤':
    'Coding rule: minimal diff + verification steps',
  '编程：最小改动，附验证步骤': 'Code: minimal change, with verification steps',
  编辑: 'Edit',
  网格: 'Grid',
  网络: 'Network',
  自动审批: 'Auto-approve',
  '自动（按内置表认，认不出按 128k）':
    'Auto (recognized from the built-in table; 128k if unknown)',
  '自定义…': 'Custom…',
  '自定义安装 MCP（stdio）': 'Install a custom MCP (stdio)',
  '自定义窗口（token）': 'Custom window (tokens)',
  萌新微光: 'Gleam newbie',
  规划: 'Plan',
  '规划 → 执行 → 反思全流程': 'Full plan → execute → reflect loop',
  视图: 'View',
  计划步骤上限: 'Cap on plan steps',
  '让创作产出更容易被大模型检索、引用与推荐。每次创作完成后自动给出建议，也可在此手动分析任意内容。':
    'Make creative output easier for large models to retrieve, cite and recommend. Suggestions are given automatically after each creative task, and you can analyze anything manually here.',
  '记录 Gleam 与你协作的每一步——任务完成、技能新增、效率提升。':
    'Records every step of your work with Gleam — tasks finished, skills added, efficiency gained.',
  记忆与上下文: 'Memory and context',
  记忆容量与上下文压缩: 'Memory capacity and context compression',
  记忆条目: 'Memory entries',
  设置与数据: 'Settings and data',
  设置分区: 'Settings sections',
  询问审批: 'Ask for approval',
  请我批准: 'Ask me first',
  '请求超时（秒）': 'Request timeout (s)',
  '读取本机其他 AI 工具留下的记忆与规则文件，选中后按段落写进 Gleam 的记忆库。只读，不改动它们的文件。':
    "Read memory and rule files left by other AI tools on this machine; the selection is written into Gleam's memory store paragraph by paragraph. Read-only — their files are never modified.",
  账户与订阅: 'Account and subscription',
  '超出改为能力菜单 + 按需筛选':
    'Overflow becomes a capability menu with on-demand filtering',
  超时未答复将自动拒绝:
    'A request that times out with no answer is declined automatically',
  超时重试次数: 'Retries on timeout',
  '跨会话记住你的偏好、项目背景与知识沉淀。':
    'Remembers your preferences, project background and accumulated knowledge across chats.',
  '跳过，稍后配置': 'Skip and configure later',
  '辅助模型（可选，用于上下文压缩 / GEO 分析 / 技能优化等高频辅助调用）':
    'Helper model (optional; used for context compression / GEO analysis / skill optimization and other frequent helper calls)',
  输入: 'Input',
  '输入命令，Enter 运行（每条命令都经过 Gleam 的安全门控）':
    "Type a command, Enter to run (every command goes through Gleam's safety gate)",
  运维专家: 'Operations expert',
  运行失败后自动修订参数并保存新版本:
    'Revise the arguments and save a new version after a failed run',
  '近 7 天 token': 'Tokens in the last 7 days',
  返回: 'Back',
  返回应用: 'Back to app',
  '返回（Ctrl+[）': 'Back (Ctrl+[)',
  '还没有 Supabase？点此填写项目连接（进阶）':
    'No Supabase yet? Click to enter a project connection (advanced)',
  '还没有事件。跑一个目标，这里会一行行走过它调用的工具。':
    'No events yet. Run a goal and this will walk through every tool it calls.',
  还没有地址: 'No addresses yet',
  这个筛选下没有任务: 'No tasks under this filter',
  '这张表回答的不是「我还能连什么」，而是「我允许了什么」：每一条常驻边界通到哪、谁能触发、什么东西会离开本机、留痕在哪、想关掉动哪里。内容全部由本机运行状态现算，读它本身一个包都不发。':
    'This table answers not "what else can I connect to" but "what have I allowed": where each standing boundary reaches, who can trigger it, what leaves this machine, where the trail is, and where to turn it off. Everything is computed from local runtime state — reading it sends no packets at all.',
  连接: 'Connections',
  连接与出网: 'Connections and egress',
  连续活跃天数: 'Consecutive active days',
  '退出后仅清除本机登录状态，本地的密钥与数据不会被删除。':
    'Signing out only clears the local sign-in state; local keys and data are not deleted.',
  '选一个你有账号的厂商，下一步填写 API Key。':
    'Pick a provider you have an account with, then enter your API key.',
  '选择任务工作区（文件操作将限定在该文件夹内）':
    'Choose a task workspace (file operations are confined to this folder)',
  '选择工作区后，终端会在主文件夹中运行。':
    'Once you choose a workspace the terminal runs inside it; until then it runs in your home folder.',
  选择或切换工作区: 'Choose or switch workspace',
  选择模型厂商: 'Choose a model provider',
  通用助手: 'General assistant',
  '通用：规划 → 执行 → 复盘': 'General: plan → execute → review',
  邮箱: 'Email',
  重做: 'Redo',
  重启后生效: 'Takes effect after restart',
  重命名空间: 'Rename space',
  重新扫描: 'Rescan',
  重新扫描工作区里的站点: 'Rescan sites in the workspace',
  重新算一遍: 'Recompute',
  重新计算候补目标: 'Recompute follow-up goals',
  重规划上限: 'Replan cap',
  钩子: 'Hooks',
  长期记忆上限: 'Long-term memory cap',
  门控留痕: 'Safety-gate trail',
  '防止"跑飞"：卡住了要能识别，大输出要先处理再往下传。':
    'Prevents runaways: getting stuck must be detectable, and large output is processed before being passed on.',
  集成: 'Integrations',
  需我批准: 'Needs my approval',
  '需要账号同步才能看到别人共享的站点，这一版还没有':
    "Seeing sites others shared with you needs account sync, which this version doesn't have yet",
  面板: 'Panel',
  面板坞: 'Panel dock',
  页面导航: 'Page navigation',
  项目经理: 'Project manager',
  '项目规划、任务拆解、进度跟踪与风险管理':
    'Project planning, task breakdown, progress tracking and risk management',
  '预算闸 · 未触发': 'Budget · not tripped',
  预览页面: 'Preview page',
  高效: 'Efficient',
  默认空间: 'Default space',
  '条 · 可撤销': 'undoable',
  'API Key 已清除': 'API key cleared',
  'API 类型': 'API type',
  'GEO 优化建议': 'GEO suggestions',
  'GEO 可见度分析': 'GEO visibility analysis',
  'GOROOT 环境变量': 'GOROOT environment variable',
  'Gleam 会在需要你决定时提醒你':
    'Gleam will tell you when it needs a decision',
  'Gleam 图标': 'Gleam icon',
  'Gleam 自己': 'Gleam itself',
  'Go 已就绪：': 'Go is ready:',
  'Go 检测成功！': 'Go detected!',
  'Key 无效或已过期': 'Key is invalid or expired',
  'Mock 模型': 'Mock model',
  上一级: 'Up one level',
  上一组: 'Previous group',
  上下文窗口读数不可用: 'Context window reading unavailable',
  上次看的: 'Last viewed',
  下一组: 'Next group',
  '下载并解压中…': 'Downloading and extracting…',
  不使用: "Don't use",
  不可还原: "Can't be restored",
  '不想用整套主题时，只换强调色。':
    "Swap only the accent color if you don't want a full theme.",
  不改变: 'Unchanged',
  不绑定: 'Unbound',
  不限制: 'Unlimited',
  丢弃并删除: 'Discard and delete',
  '为任务创建 worktree': 'Create a worktree per task',
  主模型: 'Main model',
  事件: 'Events',
  云端用户: 'Cloud user',
  云端连接已保存: 'Cloud connection saved',
  人设设置: 'Persona',
  代理: 'Proxy',
  代理地址: 'Proxy address',
  代理方式: 'Proxy mode',
  以后再说: 'Later',
  任务与工具: 'Tasks and tools',
  '任务名与“到点要做什么”都要填哦':
    'Both a task name and what to do are required',
  '任务回复与通知里助手的称呼。':
    'How the assistant is addressed in task replies and notifications.',
  任务完成: 'Task complete',
  任务已恢复: 'Task resumed',
  任务已暂停: 'Task paused',
  任务类型: 'Task type',
  任意: 'Any',
  份记忆: 'memories',
  会离开本机: 'Leaves this machine',
  你手填的: 'Set by you',
  你指定的路径: 'The path you specified',
  使用中: 'In use',
  '例如 qwen3-max': 'e.g. qwen3-max',
  供应商: 'Provider',
  '保存中…': 'Saving…',
  保存为技能: 'Save as skill',
  修改: 'Change',
  停止: 'Stop',
  停用: 'Disable',
  先写下想让它记住的事: 'Write down something you want it to remember',
  '先选择一个工作区，才能打开里面的文件':
    'Choose a workspace first to open its files',
  兜底值: 'Fallback',
  全局唤起快捷键: 'Global summon shortcut',
  全部删除: 'Delete all',
  '全部删除？': 'Delete everything?',
  '关于 Gleam': 'About Gleam',
  关闭标签: 'Close tab',
  关闭窗口: 'Close window',
  其他强调色: 'Other accents',
  内置: 'Built-in',
  内置表: 'Built-in table',
  再点一次确认清除: 'Click again to confirm clearing',
  写前与现在的逐行对比: 'Line-by-line diff of the before and after',
  '写前内容未留存，无法还原':
    "The original content wasn't kept, so it can't be restored",
  写前对比: 'Before-image diff',
  分支: 'Branch',
  分支前缀: 'Branch prefix',
  '分析中…': 'Analyzing…',
  分析失败: 'Analysis failed',
  '分析完成，已存入历史': 'Analysis complete, saved to history',
  '切换「完全访问」': 'Toggle "Full access"',
  切换全屏: 'Toggle full screen',
  创作自动分析: 'Auto-analyze creations',
  创建与清理: 'Create and clean up',
  '创建前先 fetch': 'Fetch before creating',
  创建并进入: 'Create and enter',
  删除: 'Delete',
  删除任务: 'Delete task',
  删除对话: 'Delete chat',
  删除技能: 'Delete skill',
  '删除模型配置？': 'Delete this model configuration?',
  删除空间: 'Delete space',
  '删除空间（对话移入默认空间）':
    'Delete space (chats move to the default space)',
  删除记忆: 'Delete memory',
  '删除这个 Model ID': 'Delete this model ID',
  '删除这条任务？': 'Delete this task?',
  '删除这条记忆（不再参与检索）':
    'Delete this memory (it stops being retrievable)',
  副本不能直接删除: "The copy can't be deleted directly",
  办公: 'Office',
  动过: 'Changed',
  助手: 'Assistant',
  助手名称已保存: 'Assistant name saved',
  协作风格已保存: 'Tone saved',
  厂商: 'Provider',
  厂商列表: 'Provider list',
  厂商服务异常: 'Provider service error',
  '厂商服务暂不可用（5xx）': 'Provider service unavailable (5xx)',
  历史: 'History',
  去发布页: 'Go to releases',
  去填写: 'Go fill it in',
  去定时任务: 'Go to schedules',
  去市场看看: 'Browse the market',
  去首页挑个模板: 'Pick a template on the home screen',
  '参数不是合法 JSON': 'Arguments are not valid JSON',
  '发现新版本 v': 'New version v',
  发送方式: 'Send method',
  取消: 'Cancel',
  取消拆分: 'Unsplit',
  另有: 'Also',
  可以开始下一个目标: 'Ready for the next goal',
  台: 'units',
  '同目录的 .old 文件': 'the .old file in the same folder',
  名称不能为空: "Name can't be empty",
  启用: 'Enable',
  启用后重新连接并挂回工具:
    'Reconnect and re-register the tools after enabling',
  命令不能为空: "Command can't be empty",
  '在 GEO 板块查看': 'View in the GEO section',
  '在下方输入第一条消息开始。': 'Type your first message below to start.',
  '复制 curl 命令': 'Copy curl command',
  '复制失败，路径是：': 'Copy failed; the path is: ',
  '复制失败：浏览器不允许访问剪贴板':
    "Copy failed: the browser won't let us read the clipboard",
  复制路径: 'Copy path',
  外观已更新: 'Appearance updated',
  失败: 'Failed',
  失败原因: 'Failure reason',
  如何补齐: 'How to complete it',
  始终开启: 'Always on',
  始终强制推送: 'Always force push',
  安全设置: 'Safety',
  '安装 Go 工具链': 'Install the Go toolchain',
  定时任务已创建: 'Schedule created',
  定时任务调度: 'Schedule',
  实际大小: 'Actual size',
  实验功能可能不稳定: 'Experimental features may be unstable',
  审批与调度: 'Approvals and scheduling',
  审批超时: 'Approval timeout',
  审核模型加拦: 'Blocked by the review model',
  '密码至少 6 位': 'Password must be at least 6 characters',
  对比: 'Diff',
  '对话与页面内容的最大宽度。': 'Max width for chat and page content.',
  对话回答后独立自检: 'Self-check chat answers after replying',
  对话已删除: 'Chat deleted',
  '导入选中（': 'Import selected (',
  将下载: 'Will download',
  展开: 'Expand',
  展示方式: 'Display',
  已保存: 'Saved',
  '已保存，下次建分支生效': 'Saved; applies the next time a branch is created',
  '已保存，会附在编程任务的指引里':
    'Saved; attached as guidance for coding tasks',
  '已保存，重启 Gleam 后生效': 'Saved; takes effect after Gleam restarts',
  已做好: 'Done',
  '已停用，工具已从注册表摘掉':
    'Disabled; the tool was removed from the registry',
  已关闭: 'Off',
  已关闭创作自动分析: 'Auto-analyze creations turned off',
  已写入长期记忆: 'Saved to long-term memory',
  已删除: 'Deleted',
  已压缩早期上下文: 'Earlier context compressed',
  已取消: 'Cancelled',
  '已在系统浏览器打开。': 'Opened in your system browser.',
  已复制: 'Copied',
  已复制路径: 'Path copied',
  已存在跳过: 'Skipped because it already exists',
  已导入: 'Imported',
  已开启: 'On',
  '已开启 · 再点一次取消': 'On · click again to cancel',
  已打开的工作区: 'Open workspaces',
  已拦截: 'Blocked',
  已改为不指定工作区: 'Now set to no workspace',
  已放行: 'Allowed',
  '已断开 SSH': 'SSH disconnected',
  已是最新版本: 'Already up to date',
  '已更新到 v': 'Updated to v',
  已添加: 'Added',
  '已清空 GEO 历史': 'GEO history cleared',
  已清除: 'Cleared',
  已登录: 'Signed in',
  已移除辅助模型: 'Helper model removed',
  已经装过了: 'Already installed',
  已触发: 'Tripped',
  '已设置，留空表示不修改': 'Set; leave empty to keep it unchanged',
  '已请求停止，收尾后会把结果写在原处':
    'Stop requested; the result will be written where it belongs once it winds down',
  已还原: 'Restored',
  已连接: 'Connected',
  已退出登录: 'Signed out',
  已配置: 'Configured',
  常用: 'Common',
  常见安装目录: 'Common install locations',
  平台: 'Platform',
  应用图标: 'App icon',
  建目录: 'Create folder',
  开关: 'Switch',
  开始执行: 'Start',
  开始检测: 'Start check',
  '引擎实时发出的事件列表与计数。':
    'The live event list and counts emitted by the engine.',
  引擎设置: 'Engine',
  引用一个历史目标: 'Reference a past goal',
  引用一个插件: 'Reference a plugin',
  强制重新载入: 'Force reload',
  当前: 'Current',
  当前任务已处理完毕: 'The current task is done',
  当前工作区: 'Current workspace',
  当前环境不支持页内查找: "Find-in-page isn't supported here",
  当前生效: 'In effect',
  当前计划: 'Current plan',
  当前账户: 'Current account',
  录制: 'Record',
  待优化: 'Needs tuning',
  循环: 'Loop',
  '思考中…': 'Thinking…',
  恢复按原计划执行: 'Resume the original plan',
  恢复默认: 'Restore defaults',
  想关掉动哪里: 'Where to turn it off',
  '我了解风险，显示实验功能':
    'I understand the risk — show experimental features',
  我知道了: 'Got it',
  所有类型: 'All types',
  手动: 'Manual',
  打开记忆页: 'Open the memory page',
  执行中: 'Running',
  '执行中…': 'Running…',
  '执行前始终询问（点击切换）': 'Always ask before running (click to switch)',
  扫描到: 'Found',
  扫描层级: 'Scan depth',
  批准执行: 'Approve and run',
  技能使用: 'Skill usage',
  技能名不能为空: "Skill name can't be empty",
  技能已删除: 'Skill deleted',
  技能新增: 'Skill added',
  技能自进化: 'Skill self-improvement',
  '把所有快捷键恢复为默认值？': 'Restore all shortcuts to their defaults?',
  '拉取中…': 'Fetching…',
  拉取可用模型: 'Fetch available models',
  拉取失败: 'Fetch failed',
  拒绝: 'Decline',
  '按下组合键…（Esc 取消）': 'Press a key combination… (Esc to cancel)',
  按计划执行: 'Follow the plan',
  按量: 'Pay as you go',
  按需检索: 'Retrieve on demand',
  接入说明: 'Setup notes',
  接口不存在: 'Endpoint not found',
  '接口地址（Base URL）': 'Base URL',
  接口返回错误: 'Endpoint returned an error',
  推理: 'Reasoning',
  提交说明: 'Commit message',
  提交说明指令: 'Commit message instructions',
  提示: 'Tip',
  插件: 'Plugins',
  搜索已安装的扩展: 'Search installed extensions',
  搜索已归档任务: 'Search archived tasks',
  搜索扩展: 'Search extensions',
  摘要已清空: 'Summary cleared',
  收起: 'Collapse',
  收起站点模板: 'Hide site templates',
  '收起（可在「站点」页重新打开）': 'Hidden (reopen it from the Sites page)',
  放大: 'Zoom in',
  放弃更改: 'Discard changes',
  '放弃未保存的模型配置？': 'Discard the unsaved model configuration?',
  效率提升: 'Efficiency gained',
  数据只在这台电脑: 'Data stays on this computer',
  数据目录: 'Data directory',
  数量上限: 'Count cap',
  文件操作不受工作区限制: "File operations aren't limited to the workspace",
  断开连接: 'Disconnect',
  新建: 'New',
  新建工作区: 'New workspace',
  '新建工作区…': 'New workspace…',
  新标签页: 'New tab',
  无密钥: 'no key',
  无需建立索引: 'No index needed',
  日一二三四五六: 'SMTWTFS',
  显示: 'Show',
  显示密钥: 'Show key',
  显示的模块: 'Visible modules',
  智能体套餐: 'Agent plan',
  暂不支持: 'Not supported yet',
  暂不绑定文件夹: 'Not binding a folder yet',
  '暂停后到点不再执行，随时可恢复':
    "While paused it won't run when due; you can resume any time",
  '暂无 SSH 连接': 'No SSH connections yet',
  暂无已归档任务: 'No archived tasks',
  暂未配置模型: 'No model configured yet',
  更多操控: 'More controls',
  更换模型: 'Change model',
  更新完成: 'Update complete',
  最近一轮改动: 'Latest round of changes',
  有一个操作在等你决定: 'An action is waiting for your decision',
  有审批或异常任务值得查看: 'There are approvals or failed tasks worth a look',
  有改动: 'Has changes',
  '有新的实验功能时会出现在这里。':
    'New experimental features will show up here.',
  '有新的审批请求，请前往目标视图':
    'New approval request — go to the Goals view',
  未命名内容: 'Untitled',
  未命名空间: 'Untitled space',
  未接入: 'Not connected',
  未知: 'Unknown',
  未知版本: 'Unknown version',
  未设置密钥: 'No key set',
  未达标说明: 'Why it fell short',
  未配置: 'Not configured',
  未配置模型: 'No model configured',
  本地版: 'Local build',
  本机没有: 'This machine has none',
  本机版本: 'Local version',
  '本轮已结束，未等到你的决定': 'This round ended before you decided',
  来源未知: 'Unknown source',
  枚举: 'Enum',
  查看执行详情: 'View execution details',
  校验并添加: 'Validate and add',
  根目录: 'Root folder',
  '根目录（未选择）': 'Root folder (none selected)',
  桌面工具: 'Desktop tools',
  档: 'tier',
  '检测中…': 'Checking…',
  '模型、工作区与本机资源读数。':
    'Readings for the model, workspace and local resources.',
  模型档位: 'Model tiers',
  模型设置: 'Model',
  模型配置已删除: 'Model configuration deleted',
  '模拟点击与键盘输入来操作其他应用。':
    'Operate other apps by simulating clicks and key presses.',
  '正在下载…': 'Downloading…',
  '正在向厂商拉取模型列表…': 'Fetching the model list from the provider…',
  '正在执行…': 'Running…',
  '正在拉取模型列表…': 'Fetching the model list…',
  '正在探测…': 'Probing…',
  '正在读取…': 'Reading…',
  正在载入: 'Loading',
  每隔: 'Every',
  毛玻璃效果: 'Frosted glass',
  没有匹配: 'No matches',
  没有匹配的任务: 'No matching tasks',
  没有匹配的扩展: 'No matching extensions',
  没有待压缩的早期对话: 'No earlier conversation waiting to be compressed',
  '没有找到 SSH 配置文件': 'No SSH config file found',
  '没有找到本机 SSH 配置文件。': 'No local SSH config file found.',
  没有智能体: 'No agents',
  注册失败: 'Sign-up failed',
  '注册失败（可能被别的程序占用了）':
    'Registration failed (another program may have taken it)',
  注册并登录: 'Sign up and sign in',
  '浏览器拒绝了这一步，请改用键盘快捷键':
    'The browser refused this step; use a keyboard shortcut instead',
  浏览器拒绝进入全屏: 'The browser refused full screen',
  深度扫描: 'Deep scan',
  添加: 'Add',
  '添加 Model ID': 'Add model ID',
  '添加 SSH 连接': 'Add SSH connection',
  添加模型: 'Add model',
  清除: 'Clear',
  点击关闭: 'Click to close',
  现场栏默认状态: 'Default state of the live rail',
  用户手动还原: 'Restored manually by you',
  用户拒绝: 'Declined by you',
  界面语言: 'Language',
  留痕在哪: 'Where the trail is',
  留空表示辅助调用也用主模型:
    'Leave empty to use the main model for helper calls too',
  登录成功: 'Signed in',
  目标已提交: 'Goal submitted',
  直聊: 'Chat',
  '直连：代理已关闭，环境变量不再生效。':
    'Direct: the proxy is off and environment variables no longer apply.',
  知道了: 'Got it',
  确定: 'OK',
  确认重装: 'Confirm reinstall',
  秒: 'seconds',
  移动: 'Move',
  移除: 'Remove',
  '移除后辅助调用改用主模型。':
    'Helper calls fall back to the main model after removal.',
  移除工具服务: 'Remove the tool service',
  移除引用: 'Remove reference',
  移除站点: 'Remove site',
  '移除辅助模型？': 'Remove the helper model?',
  空间已创建: 'Space created',
  '空间已删除，对话已移入默认空间':
    'Space deleted; chats moved to the default space',
  空间已重命名: 'Space renamed',
  立即更新: 'Update now',
  第二个作为辅助模型: 'the second one as the helper model',
  等你批准: 'Waiting for your approval',
  管理: 'Manage',
  类型: 'Type',
  粘贴并匹配样式: 'Paste and match style',
  '系统 PATH': 'System PATH',
  终态: 'Final state',
  经济: 'Economy',
  绑定主题: 'Match theme',
  给这个对话起个名字: 'Give this chat a name',
  继续完善: 'Keep improving',
  继续编辑: 'Keep editing',
  编程套餐: 'Coding plan',
  编辑个人资料: 'Edit profile',
  编辑模型: 'Edit model',
  缩小: 'Zoom out',
  网络不通: 'Network unreachable',
  自动下载安装: 'Download and install automatically',
  自动删除: 'Auto-delete',
  自动压缩已关闭: 'Auto-compression off',
  自动放行: 'Allowed automatically',
  自动检测本机与设置里的主机:
    'Auto-detect hosts on this machine and in settings',
  自定义: 'Custom',
  '获取 API Key': 'Get an API key',
  获取失败: 'Fetch failed',
  触发一次: 'Trigger once',
  记忆已删除: 'Memory deleted',
  记忆开关: 'Memory switch',
  记忆文件: 'Memory files',
  记忆设置: 'Memory',
  '设置…': 'Settings…',
  语义: 'Semantic',
  '请先填写 API Key': 'Enter an API key first',
  请先描述目标: 'Describe a goal first',
  请先进入一个文件夹: 'Open a folder first',
  请确认: 'Please confirm',
  请输入: 'Please enter',
  '请输入 API Key': 'Please enter an API key',
  读不到内容: "Couldn't read the content",
  读取本地数据信息失败: 'Failed to read local data info',
  谁能触发: 'Who can trigger it',
  调用: 'Calls',
  账号: 'Account',
  账户: 'Account',
  跟随窗口宽度: 'Follow window width',
  跳到这个任务: 'Jump to this task',
  轻量扫描: 'Light scan',
  辅助: 'Helper',
  辅助模型: 'Helper model',
  '辅助模型（可选）': 'Helper model (optional)',
  '辅助模型，例如一个更快的小模型': 'Helper model, e.g. a faster small one',
  运行: 'Run',
  运行中: 'Running',
  运行方式: 'Run mode',
  还原: 'Restore',
  还原改动: 'Revert the changes',
  还原面板宽度: 'Reset panel width',
  还没有任务: 'No tasks yet',
  还没有可审阅的改动: 'No changes to review yet',
  还没有可触发的钩子: 'No hooks to trigger yet',
  还没有安装插件: 'No plugins installed yet',
  还没有技能: 'No skills yet',
  还没有空间: 'No spaces yet',
  这个版本没有可开关的实验功能:
    'This version has no experimental features to toggle',
  这台电脑没有可用的桌面工具: 'No desktop tools available on this computer',
  这是一个空对话: 'This chat is empty',
  这次更新了什么: "What's in this update",
  这里不能粘贴: "Can't paste here",
  '这里现在建不了 worktree': "Can't create a worktree here right now",
  '连接 SSH': 'Connect SSH',
  '连接 SSH…': 'Connect SSH…',
  '连接中…': 'Connecting…',
  连接失败: 'Connection failed',
  '连接暂时中断，正在自动重连…':
    'Connection lost — reconnecting automatically…',
  连接检测: 'Connectivity check',
  连接超时: 'Connection timed out',
  退出: 'Quit',
  退出码: 'Exit code',
  选一个文件夹作为工作区: 'Pick a folder as your workspace',
  选择任务工作区: 'Choose the task workspace',
  选择工作区文件夹: 'Choose a workspace folder',
  选择当前文件夹: 'Choose this folder',
  选择要引用的文件: 'Choose files to reference',
  通到哪: 'Where it reaches',
  邮箱格式看起来不对: "That email doesn't look right",
  部分完成: 'Partially complete',
  配置不完整: 'Incomplete configuration',
  '配置不完整：先选择厂商或填写模型名':
    'Incomplete configuration: pick a provider or enter a model name first',
  '配置成功，开始使用吧': "Configured — you're ready to go",
  '配置文件里没有具体的 Host': 'The config file has no concrete Host entries',
  配置有误: 'Misconfigured',
  配置来源: 'Config source',
  采纳时填进输入区的就是这句话:
    'This exact text goes into the input when you accept it',
  里程碑: 'Milestones',
  重命名: 'Rename',
  重命名对话: 'Rename chat',
  重命名微光空间: 'Rename space',
  重新读取: 'Re-read',
  重新载入: 'Reload',
  重装: 'Reinstall',
  重装它: 'Reinstall it',
  重试: 'Retry',
  重连: 'Reconnect',
  '随便聊点什么…': 'Say something…',
  随时可能变化: 'May change at any time',
  隐藏侧边栏: 'Hide sidebar',
  隐藏密钥: 'Hide key',
  需要配置模型密钥: 'Needs a model key',
  静态检查: 'Static check',
  '项目地址和 anon key 都要填':
    'Both the project URL and the anon key are required',
  项目级记忆: 'Project-level memory',
  预算: 'Budget',
  预算闸: 'Budget',
  预览: 'Preview',
  颜色模式: 'Color mode',
  验收清单: 'Acceptance list',
  '高级：接入参数、生成参数与模型档位':
    'Advanced: endpoint, generation parameters and model tiers',
  默认: 'Default',
  默认执行权限: 'Default access',
  鼠标与键盘操控: 'Mouse and keyboard control',
  '自动审批：仅高风险操作才征求批准（点击切换）':
    'Auto-approve: only high-risk actions ask for approval (click to switch)',
  '完全访问：高风险操作仍会询问（点击切换）':
    'Full access: high-risk actions still ask (click to switch)',
  'Gleam 不预先给代码建索引：需要时用 file.search / file.list 在当前工作区里现查，所以没有索引要等、也没有索引要清。':
    "Gleam doesn't pre-index code: file.search / file.list query the current workspace on demand, so there's no index to wait for and none to clear.",
  'Gleam 在本机运行，没有订阅计划；模型费用由你配置的厂商直接结算。':
    'Gleam runs locally with no subscription; model fees are billed directly by the provider you configure.',
  'Gleam 完全在本机运行，你的数据不会离开这台电脑。':
    'Gleam runs entirely on this machine; your data never leaves this computer.',
  'Gleam 用一套主模型执行任务，可另配一个更快更便宜的辅助模型处理压缩、复核等高频小调用。':
    'Gleam runs tasks on one main model, and can use a faster, cheaper helper model for compression, reviews and other frequent small calls.',
  'Gleam 的技能是「可执行的步骤清单」，别的工具留下的是 Markdown 说明——两者不是同一种东西，硬导进来只会得到一个空壳，所以这里只列出、不导入。':
    "Gleam skills are executable step lists, while other tools leave Markdown notes — they aren't the same thing, and forcing an import would only produce an empty shell, so they're listed here but not imported.",
  'Gleam 的长期记忆与会话上下文都保存在本机数据目录里。':
    "Gleam's long-term memory and conversation context are stored in the local data directory.",
  'Gleam 目前只有这一款图标。': 'Gleam currently has just this one icon.',
  'Gleam 能在这台电脑上做的桌面动作：读写剪贴板、截屏、发系统通知、插入文本片段。每个动作的放行档位都可以单独调。':
    "Desktop actions Gleam can perform on this computer: read and write the clipboard, take screenshots, send system notifications, insert text snippets. Each action's access level can be set separately.",
  'Gleam 访问模型服务时的连通情况与代理方式。':
    "Connectivity and proxy mode for Gleam's calls to model services.",
  模板分类: 'Template category',
  '例如：使用 Conventional Commits，说明写中文。':
    'e.g. use Conventional Commits; write the message in Chinese.',
  '例如：官网改版、学习笔记': 'e.g. website redesign, study notes',
  '如：report、*.md': 'e.g. report, *.md',
  '做出来的网页会放进工作区 sites/，并出现在「站点」页':
    'Finished pages go into the workspace sites/ folder and show up on the Sites page',
  '权限级别：只读自动放行 / 需我批准 / 完全访问（始终审批）':
    'Access level: read-only auto-allowed / needs my approval / full access (always approve)',
  '用这个模板起一个站点（只填进输入框，不会自动提交）':
    'Start a site from this template (it only fills the input; nothing is submitted automatically)',
  '· 「定时任务」用中文说时间即可自动执行，无需任何技术语法':
    '· For Schedules, describe the timing in plain words — no technical syntax needed',
  '· 在底部输入框直接说要做什么，例如“整理当前文件夹”':
    '· Type what you want done in the input below, e.g. "tidy the current folder"',
  '· 模型密钥在「全部设置 → 模型」填写，只存本机':
    '· Enter model keys under All settings → Model; they stay on this machine',
  '· 高风险操作（删除/执行命令）会先征求你的同意':
    '· High-risk actions (deleting, running commands) ask for your consent first',
  '为每个任务开一个 git worktree：任务在副本目录里改文件，主工作区全程不动。改动留在任务分支上，不会自动并回主工作区。':
    'Open a git worktree per task: the task edits files in a copy directory and your main workspace never moves. Changes stay on the task branch and are not merged back automatically.',
  '主题、颜色模式与排版。外观偏好只保存在本机。':
    'Theme, color mode and typography. Appearance preferences are stored locally only.',
  云端账号: 'Cloud account',
  '从 ~/.ssh/config 里挑选主机，记在这里。':
    'Pick hosts from ~/.ssh/config and note them here.',
  '从上方目录选择安装，或使用自定义接入。':
    'Install from the catalog above, or set up a custom connection.',
  '从工作区选择文件，选中后以 @ 引用插入目标。也可输入关键词模糊搜索。':
    'Pick files from the workspace; the selection is inserted as an @ reference. You can also search by keyword.',
  '从市场安装，或在对话里让 Gleam 把常用流程固化成技能。':
    'Install from the market, or have Gleam save a routine as a skill during a chat.',
  '任务执行、助手风格与后台调度。改动即时生效，重启后依然保留。':
    'Task execution, assistant tone and background scheduling. Changes apply immediately and survive restarts.',
  '任务结束后会出现在这里，重启后也读得回来。':
    'Finished tasks appear here and remain readable after a restart.',
  '任务跑完后自动删掉没有未提交改动的副本；有改动的一律保留，不会被静默丢掉。':
    'After a task finishes, copies with no uncommitted changes are deleted automatically; copies with changes are always kept and never dropped silently.',
  '会议室预约 · 内部表单': 'Meeting room booking · internal form',
  '侧栏顶部的「编程 / 通用」切换时，顺带切换的界面与权限偏好。只保存在本机。':
    'Interface and access preferences that switch along with Code / General at the top of the sidebar. Stored locally only.',
  '允许「GEO」页对你的站点做生成式搜索可见度分析（会调用模型）。':
    "Allow the GEO page to analyze your sites' visibility in generative search (makes model calls).",
  '关着：任务直接在你的工作区里执行，改动就地落盘。':
    'Off: tasks run directly in your workspace and changes land right there.',
  '关闭后已创建的定时任务都不会自动触发（仍可手动运行）。':
    "Once off, existing schedules won't trigger on their own (you can still run them manually).",
  内置默认: 'Built-in default',
  '写提交说明时的写法要求，会附在编程任务的指引里，由模型按它写。':
    'Style requirements for commit messages; attached as guidance for coding tasks so the model follows them.',
  出网: 'Egress',
  '切换界面外壳的文案；任务内容与后端消息保持原文。':
    'Switches the interface copy; task content and backend messages keep their original language.',
  '创建一个任务，让 Gleam 按时自动执行目标。':
    'Create a task so Gleam runs a goal on schedule.',
  参数: 'Arguments',
  参数说明: 'Argument reference',
  '发布会倒计时 · 单页': 'Launch countdown · single page',
  '可能出现异常结果、额外的模型调用或更高的资源占用。':
    'May produce unexpected results, extra model calls or higher resource usage.',
  可选: 'Optional',
  '右侧「现场」栏：任务运行时引擎在做什么，摊开在一列仪表里。':
    'The Live rail on the right: what the engine is doing while a task runs, laid out as a column of gauges.',
  '团队周报 · 指标看板': 'Team weekly report · metrics dashboard',
  '在任何应用里按快捷键唤出 Gleam（只在桌面版可用）。':
    'Summon Gleam from any app with a shortcut (desktop app only).',
  '在任务输入框里用哪个键发送；另一种组合用来换行。快捷键页里的同一设置会同步。':
    'Which key sends in the task input; the other combination inserts a newline. The same setting on the Shortcuts page stays in sync.',
  '城市夜骑活动 · 报名页': 'City night ride · sign-up page',
  '外部脚本、CI 或其他程序可以通过 HTTP 请求立刻触发某个定时任务。':
    'External scripts, CI or other programs can trigger a schedule immediately with an HTTP request.',
  '完成一次多步骤任务后，Gleam 会主动建议把流程固化为技能；也可以去市场直接安装。':
    'After finishing a multi-step task, Gleam suggests saving the flow as a skill; you can also install one from the market.',
  官方: 'Official',
  官方注册表: 'Official registry',
  '实验功能不保证向后兼容，正式发布前行为可能调整。':
    "Experimental features aren't guaranteed to stay backward compatible and may change before release.",
  '家庭记账 · 月度看板': 'Household budget · monthly dashboard',
  '密钥、对话、技能、工具都只存在这台电脑':
    'Keys, chats, skills and tools all stay on this computer',
  '对本来会自动放行的中高风险动作，先用辅助模型快筛一遍，被标记才交给你确认。':
    'Pre-screen medium- and high-risk actions that would otherwise be auto-allowed; only flagged ones come to you.',
  '尚未安装任何 MCP 服务器': 'No MCP servers installed yet',
  '已经结束的任务记录（保存在数据目录 tasks/ 下）。打开可查看过程和结果。':
    'Records of finished tasks (kept under tasks/ in the data directory). Open one to see how it went.',
  '引用目标只提供背景，不会自动重新执行原任务。':
    "A referenced goal only provides background; it won't re-run the original task.",
  '弹层与遮罩的背景模糊。关闭可在低配机器上更流畅。':
    'Background blur for popovers and overlays. Turn it off for smoother performance on low-end machines.',
  '当前以离线演示模型运行（启动参数 --mock-llm）。这里保存的配置会写入本机，正常启动时生效；校验仍是真实网络请求。':
    'Currently running on the offline demo model (--mock-llm). Configuration saved here is written locally and takes effect on a normal start; validation still makes real network requests.',
  '影响回复的措辞：严谨精确、温和平实或直击要点。':
    'Shapes how replies are worded: precise, gentle or straight to the point.',
  必填: 'Required',
  快速上手: 'Quick start',
  '打开上面的开关之后，下一个任务就会在自己的副本目录里执行。':
    'After you turn the switch above on, the next task runs in its own copy directory.',
  '打开后，任务沉淀的记忆带上当时的工作区，检索只回本工作区 + 全局的；关着就是全局共享（历史行为）。立即生效，不用重启。':
    'When on, memories saved by a task carry the workspace they came from, and retrieval returns this workspace plus global ones; when off, everything is shared globally (the old behaviour). Effective immediately, no restart.',
  '打开工作区后即可在任务里引用其中的文件（输入 @）。':
    'Once a workspace is open you can reference its files in a task (type @).',
  '执行前把整段计划交给主模型审一遍。逐动作的轻量扫描看不见"每步都正常、连起来却在做另一件事"；这一步看的是全局。它否决时会停下来问你，你说继续就继续。':
    "Before running, the whole plan goes to the main model for a review. A per-action scan can't see a plan where every step is fine but the sequence does something else; this step looks at the whole. If it objects, it stops and asks you — say continue and it continues.",
  技能名: 'Skill name',
  '技能运行失败后自动修订参数并保存为新版本。':
    'Revise the arguments and save a new version after a skill run fails.',
  '按规则判风险：路径越界、危险命令、敏感文件。所有动作都会经过，不能关闭。':
    "Risk is judged by rules: paths out of bounds, dangerous commands, sensitive files. Every action passes through this; it can't be turned off.",
  '推送时使用 --force-with-lease——比裸 --force 安全：远端有新提交就拒绝，不会把别人的活覆盖掉。':
    "Pushes use --force-with-lease — safer than a bare --force: it refuses when the remote has new commits, so nobody else's work gets overwritten.",
  '插件（MCP 服务器）、技能和智能体角色。安装与发现在市场里完成。':
    'Plugins (MCP servers), skills and agent roles. Installing and discovering them happens in the market.',
  '插画师 · 个人作品主页': 'Illustrator · personal portfolio',
  搜索文件名: 'Search by file name',
  '新建副本之前先同步远端（会出网，见「连接」页的远端仓库同步那一行）。':
    'Sync the remote before creating a copy (this goes online — see the remote sync row on the Connections page).',
  暂无定时任务: 'No schedules yet',
  '暂无留痕。被拦截、被放行或被审核模型标记的操作会记录在这里。':
    'No trail yet. Actions that were blocked, allowed or flagged by the review model are recorded here.',
  暂无记录: 'No records yet',
  '最新版里没有这个平台的安装包，去发布页手动下载。':
    'The latest release has no package for this platform; download it manually from the releases page.',
  最近使用: 'Recently used',
  '本地笔记应用 · 产品落地页': 'Local notes app · product landing page',
  '本机检测到的 MCP': 'MCP servers found on this machine',
  本机检测到的技能: 'Skills found on this machine',
  '极简技术博客 · 首页': 'Minimal tech blog · home',
  查不到痕迹: 'No trail found',
  '查看原始 schema': 'View raw schema',
  模型服务: 'Model services',
  '此工具不需要参数。': 'This tool takes no arguments.',
  '每一个会写文件、跑命令或出网的动作，执行前都要过 Gleam 的安全门控。':
    "Every action that writes files, runs commands or goes online passes through Gleam's safety gate first.",
  '每个定时任务都会自动得到一个 HTTP 钩子；先创建定时任务。':
    'Every schedule automatically gets an HTTP hook; create a schedule first.',
  '每个空间对应一个独立文件夹，对话按空间隔离分组。可先不绑定文件夹，之后在工作区里再设置。':
    'Each space maps to its own folder, and chats are grouped per space. You can skip binding a folder for now and set it later from the workspace.',
  '没有匹配项 — 直接输入路径或继续描述':
    'No matches — type a path directly or keep describing',
  '没有检测到 HTTPS_PROXY / HTTP_PROXY 环境变量：直接连接。':
    'No HTTPS_PROXY / HTTP_PROXY environment variables detected: connecting directly.',
  浏览器操控: 'Browser control',
  浏览文件夹: 'Browse folders',
  '添加一个模型后，Gleam 才能真正执行任务。':
    'Add a model before Gleam can actually run tasks.',
  演示: 'Demo',
  '生日派对邀请函 · 单页': 'Birthday party invitation · single page',
  '由 Gleam 创建的分支统一加上这个前缀；已经有了就不重复加。':
    "Branches created by Gleam all get this prefix; it isn't added twice.",
  '由 Gleam 管理的 Worktrees': 'Worktrees managed by Gleam',
  '由 Gleam 自己执行的分支、提交与推送（走安全门控；推送每次都要你批准）。模型在 shell.exec 里自己拼的 git 命令不归这里管。':
    'Branches, commits and pushes that Gleam itself performs (through the safety gate; every push needs your approval). Raw git commands the model assembles inside shell.exec are not covered here.',
  '由辅助模型核对回答是否达成目标，未达成如实判「部分完成」。需要配置辅助模型。':
    'A helper model checks whether the answer met the goal and honestly reports "partially complete" if it didn\'t. Requires a helper model.',
  '目标中的文件操作将限定在工作区内。切换立即生效，写入不重启。':
    'File operations in a goal are confined to the workspace. Switching applies immediately; writing needs no restart.',
  '相同参数的只读调用只跑一次，已知失败的重复调用直接跳过。':
    'Identical read-only calls run once; repeats of known failures are skipped.',
  空间名称: 'Space name',
  '窗口外的旧对话自动摘要成长期上下文，省 token 不丢前情。':
    'Old turns outside the window are summarized into long-term context — saves tokens without losing the story so far.',
  '窗口外的旧对话自动摘要，省 token 不丢上下文。':
    'Old turns outside the window are summarized automatically — saves tokens without losing context.',
  '等待你批准的操作超过这个时间未答复，将自动拒绝。':
    'An operation waiting for your approval longer than this is declined automatically.',
  '绑定文件夹（可选）': 'Bind a folder (optional)',
  '编程任务里构建、测试 Go 项目要用到的工具链。':
    'The toolchain used to build and test Go projects in coding tasks.',
  网页: 'Web pages',
  '自定义 / 手动填写': 'Custom / enter manually',
  '街头摄影 · 暗色作品集': 'Street photography · dark portfolio',
  装不了: "Can't install",
  '规划 → 执行 → 验收的阶段轨与当前步骤。':
    'The plan → execute → verify stage track and the current step.',
  '规划与执行的上限、预算熔断和循环治理。':
    'Caps on planning and execution, budget circuit breaker and loop governance.',
  '让 Gleam 在工作区里做一个网页：做好的 index.html 会出现在这里。':
    'Have Gleam build a page in the workspace: the finished index.html shows up here.',
  '让 Gleam 驱动浏览器打开网页、点击和填写表单。':
    'Let Gleam drive the browser to open pages, click and fill in forms.',
  '设备借用登记 · 内部工具': 'Equipment checkout · internal tool',
  设计: 'Design',
  说明: 'Notes',
  '读书笔记周刊 · 归档页': 'Reading notes weekly · archive page',
  '超过上限时清掉最早且干净的；有改动的一个都不清，所以实际数量可能停在上限之上。':
    'Above the cap, the oldest clean ones are cleared; none with changes are, so the actual count can sit above the cap.',
  '跑一次任务之后，这里会列出它动了哪些文件。':
    'After one task run, the files it touched are listed here.',
  '跟随窗口宽度：宽屏展开、中屏收成竖签。选「展开 / 收起」会立刻应用并记住。':
    'Follow window width: expanded on wide screens, a vertical tab on medium ones. Choosing Expand / Collapse applies and remembers immediately.',
  '跟随系统时随操作系统的深浅色自动切换。':
    "In System mode it follows the operating system's light and dark setting.",
  '还在打磨中的能力，可能不稳定，也可能在之后的版本里改动或移除。':
    'Capabilities still being polished; they may be unstable and could change or be removed in later versions.',
  '还没有分析记录。用「内容创作者」角色写点东西，或在这里手动分析一段文字。':
    'No analyses yet. Write something with the Content creator role, or analyze a piece of text manually here.',
  '还没有历史目标。': 'No past goals yet.',
  '还没有由 Gleam 管理的 worktree': 'No worktrees managed by Gleam yet',
  '这台电脑没有配置账户服务，所有数据都只保存在本机。':
    'No account service is configured on this computer, so all data is kept locally only.',
  '进入编程模式时把输入框的执行权限切到这一档；高风险操作仍会请你批准。':
    "Switch the input's access level to this one when entering Code mode; high-risk actions still ask for your approval.",
  '进入通用模式时把输入框的执行权限切到这一档。':
    "Switch the input's access level to this one when entering General mode.",
  '远程 SSH 工作区暂不支持：这里只保存主机别名，Gleam 不会连接这些主机，也不会读取任何密钥文件。':
    "Remote SSH workspaces aren't supported yet: only host aliases are stored here — Gleam won't connect to them or read any key files.",
  '远程主机与 Gleam 的出网边界。':
    'The egress boundary between remote hosts and Gleam.',
  '退出后仅清除本机登录状态，本地的密钥与数据都不会删除。确定退出？':
    'Signing out only clears the local sign-in state; local keys and data are not deleted. Sign out?',
  需配置: 'Needs configuration',
  '"完全访问"': '"Full access"',
  仍然必须经过你的批准: 'still have to go through your approval',
  '你正在把 Gleam 切换到': "You're switching Gleam to",
  允许使用当前工作区内已授权的全部工具:
    'Allow the use of every tool already authorized in the current workspace',
  '切换权限前，想先和你说几句': 'Before we switch access levels, a few things',
  '小提示：如果你不确定，可以先试试“请我批准”模式。':
    'Tip: if you\'re not sure, start with "Ask me first".',
  '我已了解风险，确认切换': 'I understand the risk — switch',
  '文件删除、命令执行等': 'File deletion, command execution and similar',
  '普通步骤会自动推进，遇到风险边界时会停下来询问你':
    'Routine steps proceed on their own; when it hits a risk boundary it stops and asks you',
  '暂不切换，我再想想': 'Not now, let me think',
  '模式，这意味着：': 'mode, which means:',
  '还没有已安装插件或技能，可前往市场添加。':
    'No plugins or skills installed yet — add some from the market.',
  '这里引用已安装的能力；需要新插件时前往市场安装。':
    'Reference installed capabilities here; install new plugins from the market.',
  高危操作: 'High-risk actions',
  '还没有可用的模型：先去「设置 → 模型」选厂商并填入密钥':
    'No model available yet: pick a provider and enter a key under Settings → Model',
  '还没有可用的模型：去「设置 → 模型」选一个厂商并填入密钥':
    'No model available yet: pick a provider and enter a key under Settings → Model',
  '描述要改的代码或要修的问题，例如：修复 smoke.sh 里的编码问题':
    'Describe the code to change or the bug to fix, e.g. fix the encoding issue in smoke.sh',
  '工作区 · ': 'Workspace · ',
  '我的 · ': 'Me · ',
  '目标 · ': 'Goal · ',
  '已登录 · ': 'Signed in · ',
  '预算闸 · ': 'Budget · ',
  '操作失败：': 'Operation failed: ',
  '搜索失败：': 'Search failed: ',
  '设置保存失败：': 'Saving settings failed: ',
  '清空失败：': 'Clear failed: ',
  '分析失败：': 'Analysis failed: ',
  '打开系统选择器失败：': "Couldn't open the system picker: ",
  '选择文件失败：': 'Choosing a file failed: ',
  '选择文件夹失败：': 'Choosing a folder failed: ',
  '没能交给系统浏览器：': "Couldn't hand off to the system browser: ",
  '已引用文件：': 'Referenced file: ',
  '已引用文件夹：': 'Referenced folder: ',
  '拉取失败：': 'Fetch failed: ',
  '获取失败：': 'Fetch failed: ',
  '更新失败：': 'Update failed: ',
  // ---- 连接台账与候补目标：app.js 现场拼的那几句。句子的本体归后端两份
  // （connections.go / cues.go 的 bi），这里只管前端自己长出来的字。
  '没有查到任何常驻边界。': 'No standing boundaries found.',
  填进输入区: 'Fill into input',
  '把这句话填进下方输入区——按回车才会开始跑':
    'Puts this line into the input box below — press Enter to start a run',
  别再提: 'Dismiss',
  '按下去之后不再提这一条，可以在下面的「已按下」里撤销':
    'Stops suggesting this one; undo it in the pressed list below',
  '这张卡已经不成立了（历史变了），重新拉一次':
    'This cue no longer applies (history changed) — refetching',
  '这张卡已经不成立了，重新拉一次': 'This cue no longer applies — refetching',
  '已填进输入区。还没有开始跑——按回车才会提交':
    'Filled into the input box. Nothing is running yet — press Enter to submit',
  '候补目标读不出来：': 'Follow-up goals are unreadable: ',
  '这句话已填好，但没能记下「已采纳」：':
    'The line is filled in, but "adopted" could not be recorded: ',
  '记下「别再提」失败：': 'Failed to record the dismissal: ',
  '撤销失败：': 'Failed to undo: ',
  '加载台账失败：': 'Failed to load the ledger: ',
  '最近的任务里没有反复出问题的地方，这一轮没什么可提的。':
    'Nothing keeps going wrong in recent tasks — nothing to suggest this round.',

  // ---- 键盘页 / 终端栏 / 任务卡 / 模型弹层 / 市场与导入：各文件现场拼的字 ----
  // 带分隔符的键是给「前缀查表」用的 head：整节点是「head+尾巴」拼出来的，
  // head 查到就把前缀换成英文、尾巴递归走一遍词条与规则。
  未连接: 'Not connected',
  替换: 'Replace',
  '按下新的组合键…': 'Press a new combo…',
  没有匹配的快捷键: 'No matching shortcuts',
  '每条命令经 Gleam 安全门控执行 · 非交互式':
    'Every command runs through the Gleam safety gate · non-interactive',
  '桌面版里所有快捷键都由 Gleam 接管。':
    'In the desktop app, every shortcut is handled by Gleam.',
  浏览器内被占用: 'Used inside the browser',
  '本机还没有已配置的模型。去设置里添加一个，或拉取厂商模型列表。':
    'No model is configured on this machine yet. Add one in Settings, or fetch the provider list.',
  '切换模型失败：': 'Switching models failed: ',
  '停用后保留命令与参数，工具暂时从注册表摘掉':
    'Disabling keeps the command and arguments; the tools are temporarily detached from the registry',
  启用后重新连接并挂回工具: 'Re-enabling reconnects and mounts the tools back',
  '该服务器无需额外参数。': 'This server needs no extra parameters.',
  '该服务器已安装。重装会用下面填的命令与参数替换现有配置；只想临时别跑，请取消后到「已安装」里点停用。':
    'This server is already installed. Reinstalling replaces its configuration with the command and arguments below; to just pause it, cancel and hit Disable under Installed.',
  没有匹配的技能模板: 'No matching skill templates',
  '安装失败：': 'Install failed: ',
  本机检测到的技能: 'Skills found on this machine',
  '没检测到 MCP 配置': 'No MCP configuration found',
  '本机这些工具都没配过 MCP 服务器。':
    'None of these local tools have MCP servers configured.',
  同名已存在: 'Same-name entry exists',
  没检测到技能: 'No skills found',
  '在 ~/.claude/skills 或 ~/.codex/skills 下放过 SKILL.md 才会出现在这里。':
    'Shows up only when a SKILL.md exists under ~/.claude/skills or ~/.codex/skills.',
  没找到可导入的记忆: 'No memories available to import',
  '只在本机找 Claude Code、Codex、Cursor 等工具写过的记忆与规则文件；它们没装过或没写过，这里就是空的。':
    'Only memory and rule files written locally by Claude Code, Codex, Cursor and the like are scanned; if none of them ever wrote one, this list is empty.',
  '已停用 · ': 'Disabled · ',
  '请我批准 · ': 'Needs my approval · ',
  '需我批准 · ': 'Needs my approval · ',
  '只读放行 · ': 'Read-only · ',
  '完全访问 · ': 'Full access · ',
  '等你批准 · ': 'Waiting for you · ',
  '自动压缩已关闭 · ': 'Auto-compress off · ',
  '绑定文件夹：': 'Bound folder: ',
  '过滤模型…': 'Filter models…',
  '手动输入模型 ID': 'Enter a model ID manually',
  '或手动输入…': 'Or enter one manually…',
  请选择模型: 'Select a model',
  手动输入: 'Enter manually',
  没有匹配的模型: 'No matching models',
  '没有可用的模型列表，请手动填写模型 ID':
    'No model list available — enter a model ID manually',
  已并入本次拉取到的模型: 'Merged with the models fetched just now',
  '内置兜底（点「拉取可用模型」可拿到该厂商的完整列表）':
    'Built-in fallback (use Fetch available models for the full list)',
  刚刚: 'just now',

  // ---- 厂商名与套餐标签 ----
  // 这些字由后端 /api/providers 带出来（内置目录写的就是中文），在界面上各是一个
  // **完整的文本节点**，按键查表即可。厂商名还决定头像里的首字母，settings2-models.js
  // 的 glyph() 同样先过一遍这里，否则英文界面会留下「智」「月」这种落单的汉字。
  'TokenDance 词元跳动（聚合）': 'TokenDance (aggregator)',
  'DeepSeek 深度求索': 'DeepSeek',
  '智谱 GLM': 'Zhipu GLM',
  '月之暗面 Kimi': 'Moonshot Kimi',
  '阿里云百炼（通义 Qwen）': 'Alibaba Cloud Model Studio (Qwen)',
  '火山方舟（豆包）': 'Volcano Ark (Doubao)',
  'MiniMax（国内站）': 'MiniMax (China)',
  'MiniMax（国际站）': 'MiniMax (International)',
  'OpenRouter（聚合）': 'OpenRouter (aggregator)',
  '阶跃星辰 StepFun': 'StepFun',
  百度千帆: 'Baidu Qianfan',
  'Z.ai（智谱国际站）': 'Z.ai (Zhipu International)',
  'Kimi（国际站）': 'Kimi (International)',
  '千问AI平台（Qwen）': 'Qwen AI Platform',
  'BytePlus ModelArk（字节国际站）': 'BytePlus ModelArk',
  'StepFun（国际站）': 'StepFun (International)',
  '百度千帆（国际站）': 'Baidu Qianfan (International)',
  '腾讯云 TokenHub': 'Tencent Cloud TokenHub',
  '腾讯云 TokenHub（国际站）': 'Tencent Cloud TokenHub (International)',
  'MuleRouter（聚合）': 'MuleRouter (aggregator)',
  '按量 Token': 'Pay-as-you-go token',
  '按量 Token（OpenAI 兼容）': 'Pay-as-you-go token (OpenAI-compatible)',
  '按量 Token（OpenAI 兼容，国内站）':
    'Pay-as-you-go token (OpenAI-compatible, China)',
  '按量 Token（OpenAI 兼容，国际站）':
    'Pay-as-you-go token (OpenAI-compatible, International)',
  '按量 Token（快速版，OpenAI 兼容）':
    'Pay-as-you-go token (fast, OpenAI-compatible)',
  '按量 Token（Chat Completions）': 'Pay-as-you-go token (Chat Completions)',
  '按量 Token（Messages API）': 'Pay-as-you-go token (Messages API)',
  '编程模式（旗舰版，复杂任务）': 'Coding mode (flagship, complex tasks)',
  'Anthropic 兼容入口': 'Anthropic-compatible endpoint',
  'Anthropic 兼容入口（Claude Code 同款）':
    'Anthropic-compatible endpoint (same as Claude Code)',
  'GLM Coding Plan 编程套餐（OpenAI 兼容）':
    'GLM Coding Plan (OpenAI-compatible)',
  'Kimi Coding（Anthropic 兼容；托管版经 Kimi Code 登录自动配置）':
    'Kimi Coding (Anthropic-compatible; the hosted plan configures itself when you sign in to Kimi Code)',
  '百炼 Coding Plan（Anthropic 兼容；Key 为 sk-sp- 开头，与按量不互通）':
    'Model Studio Coding Plan (Anthropic-compatible; sk-sp- keys stay separate from pay-as-you-go)',
  '百炼 Coding Plan（OpenAI 兼容；一个 Key 聚合千问/GLM/Kimi/MiniMax）':
    'Model Studio Coding Plan (OpenAI-compatible; one key covers Qwen/GLM/Kimi/MiniMax)',
  '方舟 Coding Plan（Anthropic 兼容，适配 Claude Code）':
    'Ark Coding Plan (Anthropic-compatible, works with Claude Code)',
  '方舟 Coding Plan（OpenAI 兼容入口）':
    'Ark Coding Plan (OpenAI-compatible endpoint)',
  'MiniMax Coding Plan（Anthropic 兼容，国内站）':
    'MiniMax Coding Plan (Anthropic-compatible, China)',
  'MiniMax Coding Plan（Anthropic 兼容，国际站）':
    'MiniMax Coding Plan (Anthropic-compatible, International)',
  'Kimi Coding（Anthropic 兼容）': 'Kimi Coding (Anthropic-compatible)',
  'Token Plan 编程订阅（Anthropic 兼容）':
    'Token Plan subscription (Anthropic-compatible)',
  'Token Plan 编程订阅（OpenAI 兼容）':
    'Token Plan subscription (OpenAI-compatible)',
  'ModelArk Coding Plan（Anthropic 兼容，适配 Claude Code）':
    'ModelArk Coding Plan (Anthropic-compatible, works with Claude Code)',
  'ModelArk Coding Plan（OpenAI 兼容入口）':
    'ModelArk Coding Plan (OpenAI-compatible endpoint)',
  'Step Plan（Anthropic 兼容，适配 Claude Code）':
    'Step Plan (Anthropic-compatible, works with Claude Code)',
  'Step Plan（OpenAI 兼容入口）': 'Step Plan (OpenAI-compatible endpoint)',
  'TokenHub Coding Plan（Anthropic 兼容，适配 Claude Code）':
    'TokenHub Coding Plan (Anthropic-compatible, works with Claude Code)',
  'TokenHub Coding Plan（OpenAI 兼容入口）':
    'TokenHub Coding Plan (OpenAI-compatible endpoint)',
  'Token Plan 订阅（Anthropic 兼容）': 'Token Plan (Anthropic-compatible)',
  'Token Plan 订阅（OpenAI 兼容）': 'Token Plan (OpenAI-compatible)',

  // ---- 模型设置页 ----
  推荐: 'Recommended',
  显示名称: 'Display name',
  '模型 ID': 'Model ID',
  默认模型: 'Default model',
  新对话默认使用这个模型: 'New chats use this model by default',
  '用于上下文压缩、AI 复核等高频小调用，选一个更快更便宜的模型':
    'Used for context compression, AI review and other frequent small calls — pick a faster, cheaper model',
  密钥已设置: 'Key set',
  '（未填模型）': '(no model)',
  激活此模型: 'Activate this model',
  '激活此模型（下一个任务开始使用）':
    'Activate this model (used from the next task)',
  设为默认: 'Set as default',
  '例如：我的 DeepSeek': 'e.g. My DeepSeek',
  'Gleam 支持同时配置多个模型，对话时下拉切换。可另配一个更快更便宜的辅助模型处理压缩、复核等高频小调用。':
    'Gleam can hold several models and you switch between them in chat. You can also add a faster, cheaper helper model for compression, review and other frequent small calls.',
  '高级：生成参数与模型档位': 'Advanced: generation parameters and model tiers',

  // ---- 外观、引导与其他静态界面 ----
  更新: 'Update',
  专家团: 'Experts',
  有可用更新: 'Update available',
  拖动调整右侧栏宽度: 'Drag to resize the right panel',
  选择安全模式: 'Choose a safety mode',
  '决定 AI 执行动作前需要多少确认。随时可在设置里改。':
    'How much confirmation the AI needs before it acts. You can change this in Settings anytime.',
  自动模式: 'Auto mode',
  '低风险动作自动执行，高风险才问你。适合日常使用。':
    'Low-risk actions run automatically; only high-risk ones ask you. Good for everyday use.',
  先规划模式: 'Plan-first mode',
  '先出完整计划，你确认后再逐步执行。更谨慎。':
    'It lays out a full plan first and only executes after you approve. More cautious.',
  交互模式: 'Interactive mode',
  '每个动作都先问你。最安全，但最慢。':
    'Every action asks you first. Safest, but slowest.',

  // ---- 连接自测（TEST_HINTS）----
  // 这些值既是独立文案，也会被拼进「模型 ID：<原因>」那一行，故两种用法都要认。
  校验失败: 'Check failed',
  '删除失败：': 'Delete failed: ',
  '鉴权失败：API Key 不正确或已过期':
    'Authentication failed: the API key is wrong or expired',
  '找不到地址或模型：核对 Base URL 与 Model ID':
    'Address or model not found: check the Base URL and model ID',
  '被限流（429），稍后再试': 'Rate limited (429) — try again later',
  '连接超时（15 秒）：检查网络或 Base URL':
    'Connection timed out (15s): check the network or the Base URL',
  '网络不通：检查 Base URL、代理或防火墙':
    'Network unreachable: check the Base URL, proxy or firewall',

  // ---- 设置页其余静态文案 ----
  '通用 · 当前': 'General · current',
  '编程 · 当前': 'Code · current',
  复制路径: 'Copy path',
  '手动：': 'Manual: ',
  '（还没填地址，保存后才生效）': '(no address yet; takes effect once saved)',
  '跟随系统：按启动 Gleam 时的 HTTPS_PROXY / HTTP_PROXY / NO_PROXY 环境变量；手动：只走下面这一条地址；不使用：直连，环境变量一律忽略。改完重启 Gleam 才切换。':
    'System: uses the HTTPS_PROXY / HTTP_PROXY / NO_PROXY environment variables present when Gleam started; Manual: only the address below; Off: direct connection, environment variables ignored. A restart is needed for the change to take effect.',

  // ---- app.js：工作台 ----
  '连不上本机的 Gleam 服务，请确认它仍在运行':
    "Can't reach the local Gleam service — make sure it's still running",
  没能加载: "Couldn't load",
  '已切换到“完全访问”，高风险操作仍会请你批准':
    'Switched to “Full access”; high-risk actions will still ask for your approval',
  '还没有填写模型 API Key，现在不填的话 AI 无法回复。要现在去填写吗？（密钥只保存在本机，下次免填）':
    'No model API key yet — without one the AI cannot reply. Fill it in now? (The key stays on this machine and is remembered next time.)',
  '（站点：产出放在当前工作区 sites/ 下的一个新文件夹里，用简短的英文文件夹名，入口是 index.html；HTML 和 CSS 写在同一个文件里，不依赖外部资源，用浏览器直接打开就能看。）':
    '(Site: put the output in a new folder under sites/ in the current workspace, using a short English folder name, with index.html as the entry point; write the HTML and CSS in one file with no external assets, so it opens directly in a browser.)',
  '插件列表暂时不可用，请稍后重试。':
    'The plugin list is unavailable right now — please try again later.',
  此目录为空: 'This folder is empty',
  目录: 'Folder',
  没有匹配文件: 'No matching files',
  '请求停止这个目标：已跑完的步骤保留，结果照常写回':
    'Asking to stop this goal: finished steps are kept and the result is written back as usual',
  '可还原 = 动这个文件之前留过一份内容；没留住的退不回去，也不假装退得回去':
    'Restorable = a copy of the content was kept before this file was touched; whatever was not kept cannot be rolled back, and we do not pretend otherwise',
  '前后没有行级差异（内容一样，或只有体量变化）':
    'No line-level difference (same content, or only a size change)',
  '（一次移动涉及的两个路径会一起退回）':
    '(A move involves two paths, and both are rolled back together)',
  '· 进行中': ' · running',
  主动提议: 'Suggestion',
  需要你的批准: 'Needs your approval',
  批准执行: 'Approve',
  拒绝: 'Deny',
  高风险: 'High risk',
  中风险: 'Medium risk',
  '✓ 已批准': '✓ Approved',
  '✗ 已拒绝': '✗ Denied',
  工具调用需要批准: 'Tool call needs approval',
  '工具调用需要你的批准，请前往目标视图处理':
    'A tool call needs your approval — handle it in the Goals view',
  '（≈ 表示部分调用未返回用量，按字数估算）':
    ' (≈ means some calls did not report usage; estimated from text length)',
  后台任务: 'Background task',
  '厂商列表加载中…': 'Loading providers…',
  '鉴权失败（401/403）：检查 API Key 是否正确或已过期':
    'Auth failed (401/403): check whether the API key is correct or expired',
  '找不到地址或模型（404）：核对 API 端点与模型名':
    'Endpoint or model not found (404): check the API endpoint and model name',
  '被限流（429）：稍后再试': 'Rate limited (429): try again later',
  '厂商侧故障（5xx）：服务暂不可用，稍后再试':
    'Provider-side failure (5xx): service temporarily unavailable, try again later',
  'API 返回错误': 'The API returned an error',
  '连接超时（15 秒）：检查网络或 API 端点':
    'Connection timed out (15 s): check your network or the API endpoint',
  '网络不通：检查端点地址、代理或防火墙':
    'Network unreachable: check the endpoint address, proxy, or firewall',
  'Mock 模型：未发起真实网络调用': 'Mock model: no real network call was made',
  'Mock 模型：没有在线模型列表': 'Mock model: no online model list',
  '上下文窗口：选了「自定义」，但还没填数值':
    'Context window: “Custom” is selected but no value is filled in',
  '清空滚动摘要与待压缩历史？短期记忆与长期记忆不受影响。':
    'Clear the rolling summary and the pending-compression history? Short-term and long-term memory are unaffected.',
  '，估算': ', estimated',
  '（估算）': ' (estimated)',
  'Gleam 自带（tools/go）': 'Bundled with Gleam (tools/go)',
  '构建技能插件、从源码编译 Gleam 需要 Go 1.22+。请在下方填写路径或运行安装脚本。':
    'Building skill plugins and compiling Gleam from source need Go 1.22+. Enter the path below or run the install script.',
  'Gleam 会依次尝试国内镜像拉取 Go 1.23.4（约 80MB），校验官方 sha256 后解压到 Gleam 自己的':
    'Gleam tries domestic mirrors in turn to fetch Go 1.23.4 (about 80 MB), verifies the official sha256, then unpacks it into Gleam’s own',
  '目录，不动机器全局 PATH。': 'folder without touching the machine-wide PATH.',
  '也可以手动装好 Go 后，在下方填写路径检测。':
    'Or install Go yourself and enter its path below to detect it.',
  自动安装没成功: 'Automatic install did not succeed',
  '检查网络或代理后点「重试」；也可以换一个镜像源再试。':
    'Check your network or proxy and hit Retry; you can also try a different mirror.',
  '或跑安装脚本：': 'Or run the install script: ',
  '或手动下载 Go：': 'Or download Go manually: ',
  '装好后在下方填路径点「检测」':
    'Once installed, enter the path below and hit Detect',
  'Go 安装路径': 'Go install path',
  '未在此路径找到 go.exe，请检查路径':
    'No go.exe found at this path — please check it',
  '从本机 SSH 配置与「设置 → 连接」里挑选一台主机。Gleam 只读 Host 名称，不读取用户名、地址或任何密钥。':
    'Pick a host from the local SSH config and Settings → Connections. Gleam reads only the Host name — never the username, address, or any key.',
  'SSH 主机': 'SSH host',
  没有可用的主机: 'No hosts available',
  '在 ~/.ssh/config 里写好 Host，或到「设置 → 连接」添加 SSH 连接。':
    'Add a Host in ~/.ssh/config, or add an SSH connection under Settings → Connections.',
  源文件夹: 'Source folder',
  '+ 添加': '+ Add',
  工作区名称: 'Workspace name',
  '输入名称...': 'Enter a name...',
  工作区图标: 'Workspace icon',
  工作区颜色: 'Workspace color',
  '建立工作区索引可增强上下文理解能力，从而提升智能体回复准确性。关于工作区索引数据处理方式，':
    'A workspace index improves context understanding and makes the agent’s replies more accurate. For how workspace index data is handled,',
  了解详情: 'learn more',
  '工作区索引还没上线：这里只先记住你的开关意向。':
    'Workspace indexing is not live yet — this only remembers your intent.',
  点击添加可读写文件夹: 'Click to add a read/write folder',
  可读写: 'Read/write',
  移除该文件夹: 'Remove this folder',
  '这里没有系统文件夹选择器，请在桌面应用里新建工作区。':
    'There is no system folder picker here — create the workspace in the desktop app.',
  '说出你想推进的事，Gleam 会边听边整理这轮会话的上下文。':
    'Say what you want to move forward; Gleam listens and organizes this session’s context as you go.',
  步骤: 'Step',
  查看原始输出: 'View raw output',
  '该任务详情已不在内存（重启后仅保留对话记录）':
    'This task’s details are no longer in memory (only the conversation record survives a restart)',
  '（无回复内容）': '(no reply content)',
  此目录下没有子文件夹: 'No subfolders in this folder',
  '注册成功，如项目开启了邮箱确认，请先到邮箱确认':
    'Signed up — if the project requires email confirmation, confirm it in your inbox first',
  '已在浏览器打开授权页，完成后自动登录…':
    'The authorization page is open in your browser; you will be signed in automatically when it is done…',
  '你的所有数据都保存在本机，不会上传云端。可随时复制路径去文件管理器查看或备份。':
    'All your data is stored on this machine and is never uploaded. Copy the path into your file manager any time to view or back it up.',
  '密钥文件（仅本机，权限仅自己可读）':
    'Key file (local only, readable by you alone)',
  '这个地址打不开：只接受 http / https。':
    'This address will not open: only http / https are accepted.',
  '还没有地址，先打开一个再谈外部浏览器。':
    'No address yet — open one before using an external browser.',
  '没弹出来：浏览器拦了弹窗，请在地址栏那里放行。':
    'It did not appear: the browser blocked the pop-up — allow it in the address bar.',
  请求过于频繁: 'Too many requests',
  '已保存，但连接测试未通过，可在设置页调整':
    'Saved, but the connection test did not pass — adjust it on the Settings page',
  '已收起站点模板，可在「站点」页重新打开':
    'Site templates collapsed — reopen them on the Sites page',
  '更新已经下载并验证。安装会退出并重新打开 Gleam。':
    'The update is downloaded and verified. Installing quits and reopens Gleam.',
  稍后更新: 'Update later',
  安装并重启应用: 'Install and restart',
  重启: 'Restart',
  '为一款本地优先的笔记应用做产品落地页：首屏标语与两个行动按钮、三项特性介绍、价格区与常见问题。':
    'Build a product landing page for a local-first notes app: a hero headline with two call-to-action buttons, three feature blurbs, a pricing section, and an FAQ.',
  '为一场城市夜骑活动做报名落地页：活动主视觉、时间地点、路线说明、报名表单（姓名、手机、车型），以及注意事项。':
    'Build a sign-up landing page for a city night-ride event: a hero visual, date and location, route notes, a sign-up form (name, phone, bike type), and a notes section.',
  '做一个街头摄影师的暗色作品集：顶部姓名与简介、按年份筛选、瀑布流照片网格（用占位色块代替照片）、点击放大的灯箱。':
    'Build a dark portfolio for a street photographer: name and bio at the top, filter by year, a masonry photo grid (placeholder color blocks instead of photos), and a click-to-zoom lightbox.',
  '做一个插画师的个人作品主页：大号姓名标题、三栏精选作品卡片（作品名 + 年份）、合作邀约区和联系方式，风格明亮活泼。':
    'Build a personal homepage for an illustrator: a large name title, three featured-work cards (title + year), a collaboration section, and contact details — bright and playful.',
  '做一个极简技术博客：站点名与一句话简介、文章列表（标题、日期、标签、两行摘要）、标签云和订阅入口，阅读舒适、深浅两套配色。':
    'Build a minimal tech blog: site name and one-line intro, a post list (title, date, tags, two-line summary), a tag cloud, and a subscribe link — comfortable to read, with light and dark themes.',
  '做一个读书笔记周刊的归档页：刊头、按期号排列的卡片（期号、本期书名、一句摘录）、搜索框与按年份分组，排版偏杂志感。':
    'Build an archive page for a reading-notes weekly: a masthead, cards arranged by issue (issue number, book title, one quote), a search box, and grouping by year — magazine-style typography.',
  '做一个家庭记账的月度看板：读取同目录 data.json（先生成一份示例数据并标明是示例），展示收支概览、按分类的环形图、按周的柱状图和最近流水表。':
    'Build a monthly dashboard for family budgeting: read data.json in the same folder (generate sample data first and mark it as sample), showing an income/expense overview, a donut chart by category, a bar chart by week, and a recent-transactions table.',
  '做一个团队周报指标看板：本地 data.json 驱动（先生成示例并标注），包含本周完成事项、进行中事项、阻塞项列表和一条趋势折线，适合投屏。':
    'Build a team weekly-report dashboard: driven by a local data.json (generate and label sample data first), with this week’s completed items, in-progress items, a blockers list, and a trend line — suitable for screen sharing.',
  '做一个会议室预约的内部小工具：左侧房间列表、右侧按小时的时间格，点格子弹出预约表单，数据存浏览器 localStorage，不需要后端。':
    'Build an internal meeting-room booking tool: a room list on the left, an hourly time grid on the right, a booking form that pops up when you click a slot, and data in browser localStorage — no backend.',
  '做一个设备借用登记台：设备表格（名称、编号、状态、借用人）、借出与归还按钮、按状态筛选，数据存 localStorage，可导出 CSV。':
    'Build an equipment checkout desk: a device table (name, ID, status, borrower), check-out and return buttons, filter by status, data in localStorage, and CSV export.',
  '做一张生日派对的网页邀请函：大号标题、时间地点、一段手写感的邀请语、加入日历按钮和回执表单（是否出席、人数）。':
    'Build a web invitation for a birthday party: a large title, date and location, a handwritten-feel invite line, an add-to-calendar button, and an RSVP form (attending, number of guests).',
  '做一个发布会倒计时单页：全屏深色背景、居中的天/时/分/秒倒计时（目标时间写在页面顶部的常量里）、一句标语和预约提醒按钮。':
    'Build a single-page launch countdown: a full-screen dark background, a centered days/hours/minutes/seconds countdown (target time as a constant at the top of the page), a tagline, and a reminder button.',

  // ---- chrome.js：快捷键 / 更新 / 关于 ----
  逗号: 'comma',
  '浏览器拦下了新窗口，请允许本页弹出窗口':
    'The browser blocked the new window — allow pop-ups for this page',
  '浏览器不允许网页读剪贴板，请直接按 Ctrl+V':
    'The browser does not let the page read the clipboard — press Ctrl+V instead',
  '浏览器没给剪贴板权限，请直接按 Ctrl+V':
    'The browser did not grant clipboard permission — press Ctrl+V instead',
  大小未知: 'Size unknown',
  '更新源说我们问得太频繁了，过几分钟再试。':
    'The update source says we are asking too often — try again in a few minutes.',
  '发布页上还没有正式版本，暂时无从比较。':
    'There is no formal release on the releases page yet, so there is nothing to compare against.',
  '没能连上更新源。检查一下网络；如果走代理，确认 Gleam 能出网。':
    'Could not reach the update source. Check your network; if you use a proxy, make sure Gleam can get out.',
  '桌面版（Electron 外壳 + 本机服务）':
    'Desktop (Electron shell + local service)',
  在本机运行的智能体工作台: 'An agent workbench running on this machine',
  '对话、技能、记忆与密钥都存在这台电脑上。':
    'Chats, skills, memory and keys all stay on this computer.',
  好: 'OK',
  向下还原: 'Restore down',
  '运行中…': 'Running…',
  目录切换失败: 'Failed to change directory',
  '输出可能不完整：有子进程仍持有输出管道':
    'Output may be incomplete: a child process still holds the output pipe',
  全部恢复默认: 'Restore all defaults',
  '在任务输入框按 Ctrl+Enter 发送，Enter 换行。':
    'In the task input, press Ctrl+Enter to send and Enter for a new line.',
  '至少要带 Ctrl 或 Alt（F1–F12 除外）':
    'Must include at least Ctrl or Alt (except F1–F12)',

  // ---- geo.js / growth.js ----
  '已开启：创作完成后自动给出 GEO 建议':
    'On: suggests GEO improvements automatically after you write',
  可以这样改: 'Try this',
  高: 'High',
  低: 'Low',
  '请至少输入 10 个字再分析': 'Enter at least 10 characters before analyzing',
  缺少冒号: 'missing colon',
  档位名为空: 'tier name is empty',
  模型名为空: 'model ID is empty',

  // ---- market.js / memory.js ----
  '没有匹配的 MCP 服务器': 'No matching MCP servers',
  无: 'None',
  搜索你的长期记忆: 'Search your long-term memory',
  '在右上角输入关键词回车即可；上方也可以直接写入一条新记忆。':
    'Type a keyword in the top right and press Enter; you can also write a new memory above.',
  没有相关记忆: 'No related memories',
  '换个关键词试试，或在上方写入这条你想让它记住的事。':
    'Try another keyword, or write what you want it to remember above.',
  '这条记忆与搜索词的相关度（词法相似度换算，同义词不算相关），越高越相关':
    'How relevant this memory is to the search term (lexical similarity; synonyms do not count). Higher means more relevant',

  // ---- schedules.js ----
  暂停: 'Pause',
  恢复: 'Resume',
  '任务名不能含 / 或 \\，换一个说法即可':
    'A task name cannot contain / or \\ — rephrase it',
  '请用一句话说说执行时间，例如“每天早上9点”':
    'Describe the schedule in one sentence, e.g. “every day at 9am”',

  // ---- settings2-models.js ----
  '已激活此模型，下一个任务开始用它':
    'This model is active; the next task will use it',
  '删除模型？': 'Delete model?',
  校验并保存: 'Validate and save',
  '请先填写 API Key 再拉取模型列表':
    'Fill in the API key before fetching the model list',
  '：这个入口可能不提供模型列表，从列表里选或手填即可':
    ': this endpoint may not provide a model list — pick from the list or type one in',
  '已填写的供应商、地址和密钥都不会保存。':
    'The provider, address and key you entered will not be saved.',

  // ---- settings2-pages.js ----
  '自定义智能体暂不支持，可在任务输入框里选择内置专家角色':
    'Custom agents are not supported yet — pick a built-in expert role in the task input',
  '· 任务已暂停': ' · task paused',
  清除失败: 'Failed to clear',
  '在任何应用里按这个组合把 Gleam 拉到前台。只在本机注册，不联网。':
    'Press this combination in any app to bring Gleam to the front. Registered locally only — no network.',
  '至少要带一个修饰键（Ctrl / Alt / Shift / Win）':
    'Must include at least one modifier key (Ctrl / Alt / Shift / Win)',
  '还没选工作区：没有可建副本的仓库，任务会在不限定目录的边界里执行。':
    'No workspace selected yet: there is no repository to copy, so tasks run without a bounded directory.',
  '这个功能开着：每个任务建一份副本，任务在副本里执行。':
    'This is on: each task gets its own copy and runs inside it.',
  '5 个': '5',
  '15 个': '15',
  '30 个': '30',
  '来自 ~/.ssh/config': 'From ~/.ssh/config',
  '通配符（* ?）条目不会列出。': 'Wildcard (* ?) entries are not listed.',
  '从你的 SSH 配置里挑选主机。Gleam 只读 Host 名称，不读取 HostName、用户名或任何密钥。':
    'Pick a host from your SSH config. Gleam reads only the Host name — never HostName, username, or any key.',
  '离线演示模型：没有发起网络请求':
    'Offline demo model: no network request was made',
  '先填代理地址，例如 http://127.0.0.1:7890':
    'Enter the proxy address first, e.g. http://127.0.0.1:7890',

  // ---- settings2.js ----
  '清除本机登录凭证；本地任务和设置不会被删除。':
    'Clears the local sign-in credentials; local tasks and settings are not deleted.',
  '登录后可同步账户信息；不登录也能使用全部本地功能。':
    'Signing in syncs account info; every local feature works without signing in.',
  '显示名称（只保存在本机，用于侧栏和个人资料）':
    'Display name (stored locally only, used in the sidebar and profile)',
  '· 当前': ' · current',
  '下次打开 Gleam 时按窗口宽度决定':
    'Decided by window width the next time Gleam opens',

  // ---- skills.js / tools.js ----
  '启用后重新进入技能清单，可被引用与运行':
    'Once enabled it re-enters the skill list and can be referenced and run',
  '停用后保留内容与统计，但不再进技能清单':
    'Once disabled its content and stats are kept, but it leaves the skill list',
  '该技能无需参数。': 'This skill takes no parameters.',
  成功: 'Success',
  结束: 'Finished',
  描述: 'Description',
  已覆盖: 'Overridden',
  恢复内置默认: 'Restore built-in default',
  '参数（JSON）': 'Parameters (JSON)',

  // ---- 第三轮补：前缀片段（后面还拼着运行期值，单独成句时也要认得出）与档位校验词 ----
  '已停用 ·': 'Disabled ·',
  '自动压缩已关闭 ·': 'Auto-compress off ·',
  '· 云端账号': ' · Cloud account',
  缺少冒号: 'missing colon',
  档位名为空: 'tier name is empty',
  模型名为空: 'model name is empty',
};

// 插值句子的翻译规则：整节点匹配正则后替换。只收界面自己拼的句子；
// 数字、主机名这类运行期值原样保留。
const I18N_MONTHS = [
  'Jan',
  'Feb',
  'Mar',
  'Apr',
  'May',
  'Jun',
  'Jul',
  'Aug',
  'Sep',
  'Oct',
  'Nov',
  'Dec',
];
const I18N_WEEKDAYS = {
  日: 'Sun',
  一: 'Mon',
  二: 'Tue',
  三: 'Wed',
  四: 'Thu',
  五: 'Fri',
  六: 'Sat',
};
const I18N_PATTERNS = [
  [
    /^拉到 (\d+) 个模型（已缓存，下次打开仍可见）$/,
    (m, n) => `Fetched ${n} models (cached; still visible next time)`,
  ],
  [
    /^内置兜底 \+ 上次拉取（(.+)）$/,
    (m, when) => `Built-in fallback + last fetch (${when})`,
  ],
  [/^(\d+) 分钟前$/, (m, n) => `${n} min ago`],
  [/^(\d+) 小时前$/, (m, n) => `${n} h ago`],
  [/^(\d+) 天前$/, (m, n) => `${n} d ago`],
  [/^(\d{1,2})月$/, (m, n) => I18N_MONTHS[Number(n) - 1]],
  [
    /^(\d+)月(\d+)日周(.)$/,
    (m, mo, d, wd) =>
      `${I18N_MONTHS[Number(mo) - 1]} ${Number(d)}, ${I18N_WEEKDAYS[wd] || wd}`,
  ],
  [
    /^(\d+) 个(任务|会话)$/,
    (m, n, noun) => `${n} ${noun === '任务' ? 'tasks' : 'chats'}`,
  ],
  [
    /^单日活跃(任务|会话)$/,
    (m, noun) => `Active ${noun === '任务' ? 'tasks' : 'chats'} in a day`,
  ],
  [
    /^切换到「(.+?)」时自动换成这个主题色。$/,
    (m, n) => `Accent switches to this when you switch to ${I18N_EN[n] || n}.`,
  ],
  // 组合尾缀（默认/辅助 + 无密钥）必须排在单尾缀之前，否则「· 无密钥」先把
  // 前半截当 host 吞掉，剩下的「默认」永远翻不动。
  [/^(.+) · 默认 · 无密钥$/, (m, p) => `${I18N_EN[p] || p} · default · no key`],
  [/^(.+) · 辅助 · 无密钥$/, (m, p) => `${I18N_EN[p] || p} · fast · no key`],
  [/^(.*) · 无密钥$/, (m, host) => `${I18N_EN[host] || host} · no key`],
  [
    /^上下文窗口占用 (\d+)%（模型窗口 (.+?)，(.+?)；最近一次请求 (\d+) tokens）$/,
    (m, pct, win, src, tok) =>
      `Context window ${pct}% used (model window ${win}, ${I18N_EN[src] || src}; last request ${tok} tokens)`,
  ],
  [
    /^上下文窗口占用 (\d+)%（模型窗口 (.+?)，(.+?)；最近一次请求 (\d+) tokens，估算）$/,
    (m, pct, win, src, tok) =>
      `Context window ${pct}% used (model window ${win}, ${I18N_EN[src] || src}; last request ${tok} tokens, estimated)`,
  ],
  [
    /^窗口内 (\d+)\/(\d+) 轮 · 待压缩 (\d+) 轮 · 摘要 (\d+) 字$/,
    (m, used, cap, over, chars) =>
      `In window ${used}/${cap} turns · ${over} to compress · ${chars} chars summarized`,
  ],
  [
    /^共 (\d+) 个快捷键，(\d+) 个已自定义。$/,
    (m, total, custom) => `${total} shortcuts, ${custom} customized.`,
  ],
  [
    /^过去一年的本机活动，共 (\d+) 次$/,
    (m, n) => `Local activity over the past year, ${n} events`,
  ],
  [/^来源: 系统 PATH · (.*)$/, (m, p) => `Source: system PATH · ${p}`],
  [/^预算闸 · (.+)$/, (m, rest) => `Budget · ${rest}`],
  // 候补卡的时间戳「最近一次 2026-01-02 15:04」：没有全角分隔符，前缀查表够不着。
  [/^最近一次 (.+)$/, (m, when) => `Last seen ${when}`],
  // ---- 计数与拼装句：app.js / market.js / importer.js / chrome.js 各处现拼的 ----
  // 数字典插在句中，词条够不着；一个形状一条规则，运行期数字原样保留。
  [/^(\d+) 项需要处理$/, (m, n) => `${n} items need attention`],
  [/^(\d+) 个目标正在推进$/, (m, n) => `${n} goals in progress`],
  [/^(\d+) 个工具$/, (m, n) => `${n} tools`],
  [/^(\d+) 项$/, (m, n) => `${n} items`],
  [/^(\d+) 条$/, (m, n) => `${n} entries`],
  [/^完成度 (\d+) \/ 100$/, (m, n) => `score ${n} / 100`],
  [
    /^(.+) · 完成度 (.+)\/100$/,
    (m, st, s) => `${I18N_EN[st] || st} · score ${s}/100`,
  ],
  [/^用时 (\d+) 分钟$/, (m, n) => `Took ${n} min`],
  [/^用时 (\d+(?:\.\d+)?) 秒$/, (m, n) => `Took ${n} s`],
  [/^待压缩 (\d+) 轮$/, (m, n) => `${n} turns to compress`],
  [
    /^(已连接|已停用|未连接) · (\d+) 个工具$/,
    (m, st, n) => `${I18N_EN[st] || st} · ${n} tools`,
  ],
  [
    /^信任 (需我批准|只读放行|完全访问)(?: · (\d+) 个工具)?$/,
    (m, p, n) => `Trust ${I18N_EN[p] || p}${n ? ` · ${n} tools` : ''}`,
  ],
  [
    /^(停用|启用)工具服务 (.+)$/,
    (m, on, name) => `${I18N_EN[on] || on} server ${name}`,
  ],
  [/^卸载 (.+)$/, (m, name) => `Uninstall ${name}`],
  [
    /^已卸载（移除 (\d+) 个工具）$/,
    (m, n) => `Uninstalled (${n} tools removed)`,
  ],
  [/^已启用（(\d+) 个工具）$/, (m, n) => `Enabled (${n} tools)`],
  [/^已连接（(\d+) 个工具）$/, (m, n) => `Connected (${n} tools)`],
  [
    /^(.+) 已连接（(\d+) 个工具已注册）$/,
    (m, name, n) => `${name} connected (${n} tools registered)`,
  ],
  [/^(重装|安装) (.+)$/, (m, v, name) => `${I18N_EN[v] || v} ${name}`],
  [
    /^移除工具服务「(.+?)」？移除后它提供的工具将不再可用。$/,
    (m, name) =>
      `Remove server "${name}"? Its tools will no longer be available.`,
  ],
  [/^缺 (.+)$/, (m, rt) => `Missing ${rt}`],
  [/^本机没有 (.+)$/, (m, rt) => `Not present on this machine: ${rt}`],
  [/^(.+) · 按量$/, (m, p) => `${I18N_EN[p] || p} · pay as you go`],
  [/^(.+) · 编程套餐$/, (m, p) => `${I18N_EN[p] || p} · coding plan`],
  [/^(.+) · 智能体套餐$/, (m, p) => `${I18N_EN[p] || p} · agent plan`],
  [/^(.+) · 默认$/, (m, p) => `${I18N_EN[p] || p} · default`],
  [/^(.+) · 辅助$/, (m, p) => `${I18N_EN[p] || p} · fast`],
  [
    /^(.+) · 没填密钥，现在跑不了$/,
    (m, id) => `${id} · key missing — cannot run yet`,
  ],
  [
    /^已切到 (.+?)，下一个任务开始用它$/,
    (m, id) => `Switched to ${id}; it takes over from the next task`,
  ],
  [
    /^已拉取 (\d+) 个模型，点一下就切$/,
    (m, n) => `Fetched ${n} models — click one to switch`,
  ],
  [
    /^(校验失败|鉴权失败：API Key 不正确或已过期|找不到地址或模型：核对 Base URL 与 Model ID|被限流（429），稍后再试|连接超时（15 秒）：检查网络或 Base URL|网络不通：检查 Base URL、代理或防火墙|拉取失败)（([\s\S]+)）$/,
    (m, hint, msg) => `${I18N_EN[hint] || hint} (${msg})`,
  ],
  [/^恢复默认（(.+)）$/, (m, combo) => `Restore default (${combo})`],
  [
    /^修改「(.+?)」的快捷键$/,
    (m, t) => `Change shortcut for "${I18N_EN[t] || t}"`,
  ],
  [/^已导入 (\d+) 份记忆$/, (m, n) => `Imported ${n} memories`],
  [
    /^已导入 (\d+) 份记忆，跳过 (\d+) 份$/,
    (m, n, s) => `Imported ${n} memories, skipped ${s}`,
  ],
  [/^导入选中（(\d+)）$/, (m, n) => `Import selected (${n})`],
  [
    /^已导入 (\d+) 台(?:，已存在跳过 (\d+) 台)?(?:，失败 (\d+) 台)?$/,
    (m, a, b, c) =>
      `Imported ${a}${b ? `, skipped ${b} already present` : ''}${c ? `, ${c} failed` : ''}`,
  ],
  [/^(.+?) · 环境变量 (.+)$/, (m, where, keys) => `${where} · env: ${keys}`],
  [
    /^来自 Claude Code \/ Claude Desktop \/ Cursor \/ VS Code \/ Codex 的配置文件。只读命令与参数，环境变量的值不会读出来；同名服务器已安装的会跳过，不覆盖你现在的配置。(?: 另有 (\d+) 台是 URL\/SSE 型，Gleam 目前只支持 stdio 接入，没有列出。)?$/,
    (m, skipped) =>
      `Comes from the configuration files of Claude Code / Claude Desktop / Cursor / VS Code / Codex. Commands and arguments are read-only; environment variable values are not read. Same-name servers already installed are skipped and never overwrite your configuration.${skipped ? ` ${skipped} more are URL/SSE type — Gleam currently supports stdio only, so they are not listed.` : ''}`,
  ],
  [
    /^技能「(.+?)」已经装过了。重装会把它恢复成市场模板，你改过的步骤会被替换；只想临时别用它，请取消后到技能页点停用。$/,
    (m, name) =>
      `Skill "${name}" is already installed. Reinstalling restores the market template and replaces your edited steps; to just pause it, cancel and hit Disable on the Skills page.`,
  ],
  [
    /^技能「(.+?)」已(重装|安装)，可在技能页运行$/,
    (m, name, v) =>
      `Skill "${name}" ${v === '安装' ? 'installed' : 'reinstalled'} — run it on the Skills page`,
  ],
  [
    /^占用 (\d+)% · 模型窗口 (.+?)（(.+?)，(.*?)） · 最近一次请求 (\d+) tokens(（估算）)? · 下一轮带 (\d+) 轮最近对话 · 短期窗口 (\d+)\/(\d+) 轮，待压缩 (\d+) 轮(?: · 累计已省约 (\d+) tokens)?$/,
    (m, pct, win, src, note, tok, est, carry, sa, sb, sc, saved) =>
      `Used ${pct}% · model window ${win} (${I18N_EN[src] || src}, ${note}) · last request ${tok} tokens${est ? ' (estimated)' : ''} · ${carry} turns carried next round · short window ${sa}/${sb} turns, ${sc} to compress${saved ? ` · ~${saved} tokens saved so far` : ''}`,
  ],
  [
    /^自动压缩已关闭：短期窗口 (\d+)\/(\d+) 轮，窗口外的对话不会被摘要接住，水位越线时也不会自动收紧$/,
    (m, a, b) =>
      `Auto-compress is off: short window ${a}/${b} turns; conversation beyond the window is not caught by a summary, and crossing the level line tightens nothing automatically`,
  ],
  [
    /^占用是「最近一次请求的输入 ÷ 模型窗口」。溢出窗口的旧对话随时汇总成摘要，不会丢；占用到 (\d+)%（任务已结束）或 (\d+)%（任务进行中）时会自动收紧下一轮携带的对话，压完接着把任务做完。$/,
    (m, end, mid) =>
      `Usage is "the last request's input ÷ the model window". Older turns beyond the window are summarized as they overflow — nothing is lost; at ${end}% (task finished) or ${mid}% (task running), the next round automatically carries fewer turns, and the task keeps going after compressing.`,
  ],
  // 前缀查表：界面里大量「中文前缀 + 拼出来的尾巴」（像 '工作区 · ' + 名字、'操作失败：' + 原因）。
  // 这类整节点是拼好的句子，精确匹配不到；这里把第一个分隔符前的那段当键去查表，
  // 查得到就换掉前缀、保留尾巴（尾巴多是运行期内容，本来就不该翻）。查不到就整句不翻。
  [
    /^([^：·]{1,24}(?:：| · ))([\s\S]+)$/,
    (m, head, tail) => {
      const has = (k) => Object.prototype.hasOwnProperty.call(I18N_EN, k);
      if (!has(head)) return null;
      // 尾巴也递归翻一次：「请我批准 · 编程」的尾巴是词条、「等你批准 · 8 项」的
      // 尾巴要走计数规则——尾巴里全剩运行期值时原样保留。
      return I18N_EN[head] + (i18nText(tail) || tail);
    },
  ],
  [/^钩子（(\d+)）$/, (m, n) => `Hooks (${n})`],
  // 连接自测的结果行「<模型 ID>：<原因>（HTTP 4xx）」：模型 ID 是运行期值，
  // 冒号前那段不该被当词条，所以只在前面都认不出时才接（前缀查表会主动让位）。
  [
    /^(\S+)：(.+?)(（HTTP (\d+)）)?$/,
    (m, model, why, _http, code) => {
      const en = I18N_EN[why];
      if (en == null) return null;
      return `${model}: ${en}${code ? ` (HTTP ${code})` : ''}`;
    },
  ],
  [
    /^正在校验 (.+?)（一次真实的最小请求）…$/,
    (m, model) => `Testing ${model} (one real minimal request)…`,
  ],
  [/^已保存 (.+)$/, (m, what) => `Saved ${what}`],
  [/^已删除 (.+)$/, (m, what) => `Deleted ${what}`],
  [/^已添加模型 (.+)$/, (m, what) => `Model ${what} added`],
  [
    /^删除「(.+?)」后会清除这套接入的密钥与配置。$/,
    (m, what) =>
      `Deleting “${what}” removes this endpoint's key and configuration.`,
  ],
  // 时长：设置页的秒 / 分钟下拉，以及「N 个文件」「N 条记忆」这类计数尾巴。
  [/^(\d+(?:\.\d+)?) 秒$/, (m, n) => `${n} s`],
  [/^(\d+(?:\.\d+)?) 分钟$/, (m, n) => `${n} min`],
  [/^(\d+)分钟$/, (m, n) => `${n} min`],
  [/^(\d+) 个文件$/, (m, n) => `${n} files`],
  [/^(.+) · (\d+) 个文件$/, (m, path, n) => `${path} · ${n} files`],
  [/^(.+?) 权限$/, (m, tool) => `${tool} permission`],
  [
    /^钩子没有单独的口令：任何能访问 (.*?) 的程序都能触发它。Gleam 默认只监听本机地址，不要把这个端口暴露到局域网或公网。$/,
    (m, host) =>
      `Hooks have no separate token: any program that can reach ${host} can trigger them. Gleam listens on localhost only by default — do not expose this port to the LAN or the internet.`,
  ],
  [
    /^(\d+) 条 · 可在记忆页检索、写入和删除$/,
    (m, n) =>
      `${n} entries · searchable, writable and removable on the Memory page`,
  ],
  [
    /^钩子来自定时任务列表（保存在数据目录 (.*?)）。调度开关：(已开启|已关闭)$/,
    (m, dir, sw) =>
      `Hooks come from the schedule list (stored in the data directory ${dir}). Scheduler: ${sw === '已开启' ? 'on' : 'off'}`,
  ],
  [
    /^扫描到 (\d+) 份文件。选中后按段落写入 Gleam 的记忆库；全程只读，不改动它们的文件。$/,
    (m, n) =>
      `Scanned ${n} files. The selection is written into Gleam's memory store paragraph by paragraph; read-only — their files are never modified.`,
  ],
  // ---- 第三轮：现场拼出的整句按「形状」收口 ----
  // 句子里混着运行期数字 / ID / 路径时不进词表，按形状匹配；尾巴能递归翻的就递归。
  [/^搜索 "(.+)" · (\d+) 个结果$/, (m, q, n) => `Search "${q}" · ${n} results`],
  [/^验收清单 (\d+)\/(\d+) 项通过$/, (m, a, b) => `Checklist ${a}/${b} passed`],
  [/^本次改动 (\d+) 个路径$/, (m, n) => `${n} paths changed this round`],
  [/^(.*?)\s*· 由 (.+?) 改的$/, (m, p, t) => `${p} · changed by ${t}`],
  [
    /^把「(.+?)」退回到这次任务开始前的样子？(.*)当前内容不会保留。$/,
    (m, name, pair) =>
      `Revert "${name}" to how it was before this task started? ${i18nText(pair) || pair}Current content will not be kept.`,
  ],
  [
    /^执行工具 (\d+) 次( · 进行中)?$/,
    (m, n, run) => `${n} tool runs${run ? ' · running' : ''}`,
  ],
  [
    /^其中 (\d+) 次失败( · 进行中)?$/,
    (m, n, run) => `${n} failed${run ? ' · running' : ''}`,
  ],
  [/^· (\d+) 次工具$/, (m, n) => ` · ${n} tool calls`],
  [
    /^；缓存命中 (\d+)%（(.+?) tokens）([\s\S]*)$/,
    (m, p, t, rest) =>
      `; cache hit ${p}% (${t} tokens)${i18nText(rest) || rest}`,
  ],
  [/^；(\d+) 次重试$/, (m, n) => `; ${n} retries`],
  [/^可固化为技能「(.+?)」$/, (m, n) => `Can be saved as a skill "${n}"`],
  [
    /^本次任务包含 (\d+) 个步骤，保存后可一键复用。$/,
    (m, n) => `This task has ${n} steps; save it to reuse them in one click.`,
  ],
  [/^目标完成（(.+?)\/100）$/, (m, s) => `Goal complete (${s}/100)`],
  [
    /^目标(.+?)（(.+?)\/100）$/,
    (m, st, s) => `Goal ${i18nText(st) || st} (${s}/100)`,
  ],
  [
    /^已设置，仅发往 (.+?)；留空表示不修改$/,
    (m, h) => `Already set; sent only to ${h} — leave blank to keep it`,
  ],
  [
    /^已存的密钥属于 (.+?)，换厂商需重新填写$/,
    (m, h) =>
      `The stored key belongs to ${h}; re-enter it when switching providers`,
  ],
  [
    /^已获取 (\d+) 个模型，模型框可直接点选$/,
    (m, n) => `Fetched ${n} models — pick one in the model box`,
  ],
  [/^· 已省约 (\d+) tokens$/, (m, n) => ` · ~${n} tokens saved`],
  [/^· 累计已省约 (\d+) tokens$/, (m, n) => ` · ~${n} tokens saved so far`],
  [
    /^本机配置：(.+?)（只解析 Host 名称）$/,
    (m, p) => `Local config: ${p} (reads the Host name only)`,
  ],
  [
    /^删除空间「(.+?)」？其中 (\d+) 个对话会移入「默认空间」，不会丢失。$/,
    (m, name, n) =>
      `Delete space "${name}"? Its ${n} chats move to "Default space" and are not lost.`,
  ],
  [/^确定删除空间「(.+?)」？$/, (m, name) => `Delete space "${name}"?`],
  [
    /^还有 (\d+) 个目标正在执行，完成后再开始新对话$/,
    (m, n) =>
      `${n} goals are still running — start a new chat once they finish`,
  ],
  [
    /^删除对话「(.+?)」？此操作不可恢复。$/,
    (m, t) => `Delete chat "${i18nText(t) || t}"? This cannot be undone.`,
  ],
  [
    /^共 (\d+) 个本地文件，约 ([\d.]+) KB。包含密钥、对话记录、技能、记忆、工具配置等。$/,
    (m, n, kb) =>
      `${n} local files, about ${kb} KB. They include keys, chat history, skills, memories and tool settings.`,
  ],
  [
    /^没能读到本机版本（(.+?)），检查更新没能开始，稍后再试一次。$/,
    (m, e) =>
      `Couldn't read the local version (${e}) — the update check did not start; try again later.`,
  ],
  [/^上一轮：已完成（(.+?)\/100）$/, (m, s) => `Last round: done (${s}/100)`],
  [
    /^正在载入 (.+?) · 一直空白多半是它拒绝被嵌，用「在系统浏览器打开」$/,
    (m, u) =>
      `Loading ${u} · a blank panel usually means it refuses to be embedded — use "Open in system browser"`,
  ],
  [
    /^有 (\d+) 个空间不在当前工作区范围内，切到那个空间再刷新就能扫到$/,
    (m, n) =>
      `${n} spaces are outside the current workspace scope; switch to that space and refresh to scan them`,
  ],
  [
    /^共 (\d+) 个，这里先列前 (\d+) 个$/,
    (m, a, b) => `${a} in total; the first ${b} are listed here`,
  ],
  [/^更新 (\d+)%$/, (m, n) => `Updating ${n}%`],
  // chrome.js：检查更新 / 应用更新的一串句子
  [
    /^没能问到更新情况：(.+?)。稍后再试一次。$/,
    (m, e) => `Couldn't check for updates: ${e}. Try again later.`,
  ],
  [
    /^已是最新版本 (.+?)。 发布页上还没有比它更新的版本，不用动它。$/,
    (m, v) =>
      `Already on the latest version ${v}. Nothing newer on the release page — no action needed.`,
  ],
  [
    /^本机版本 (.+?)。 ([\s\S]+)$/,
    (m, v, why) => `Local version ${v}. ${i18nText(why) || why}`,
  ],
  [
    /^发现新版本 v(.+?)，你现在是 (.+?)。$/,
    (m, latest, cur) => `New version v${latest} available; you are on ${cur}.`,
  ],
  [
    /^将下载 (.+?)（(.+?)）并覆盖当前程序本身。对话、技能、密钥都存在「本地数据」目录里，换程序不影响它们；旧版本会留一份 \.old 备份，想退回去改回扩展名即可。$/,
    (m, name, size) =>
      `Will download ${name} (${size}) and overwrite the current program. Chats, skills and keys all live in the "Local data" directory, so swapping the program does not affect them; the old version is kept as a .old backup — rename its extension to roll back.`,
  ],
  [
    /^已更新到 v(.+?)（下载的是 (.+?)）。 把 Gleam 关掉再打开，新版本就生效了。原文件留在 (.+?)，要退回旧版就把它的扩展名改回去。$/,
    (m, v, to, backup) =>
      `Updated to v${v} (downloaded ${to}). Quit Gleam and open it again for the new version to take effect. The original file is kept at ${i18nText(backup) || backup}; rename its extension to roll back.`,
  ],
  [
    /^，要退回旧版就把它的扩展名改回去。$/,
    () => `; rename its extension to roll back.`,
  ],
  [/^浏览器（本机服务 (.+?)）$/, (m, h) => `Browser (local service ${h})`],
  [
    /^这条命令需要你的批准(：([\s\S]*))?$/,
    (m, _colon, r) => `This command needs your approval${r ? `: ${r}` : ''}`,
  ],
  [/^已被「(.+?)」使用$/, (m, c) => `Already used by "${i18nText(c) || c}"`],
  [
    /^「(.+?)」的快捷键已清除$/,
    (m, t) => `Shortcut for "${i18nText(t) || t}" cleared`,
  ],
  // growth.js / importer.js / market.js
  [
    /^第 (\d+) 行「(.+?)」(.+?)（格式：档位: 模型ID）$/,
    (m, i, line, why) =>
      `Line ${i} "${line}": ${i18nText(why) || why} (format: tier: model-id)`,
  ],
  [/^([\d.]+)小时$/, (m, n) => `${n} h`],
  [/^· 环境变量 (.+)$/, (m, k) => ` · env: ${k}`],
  [
    /^信任 ([\s\S]+?)( · [\s\S]*)?$/,
    (m, p, rest) =>
      `Trust ${i18nText(p) || p}${rest ? i18nTail(rest) || rest : ''}`,
  ],
  [/^· (\d+) 个工具$/, (m, n) => ` · ${n} tools`],
  [
    /^已连接（(\d+) 个工具已注册）$/,
    (m, n) => `Connected (${n} tools registered)`,
  ],
  [/^相关度 (\d+)%$/, (m, n) => `Relevance ${n}%`],
  [
    /^删除这条记忆？ 「(.+?)」 删除后不再参与检索。$/,
    (m, x) =>
      `Delete this memory? "${x}" — once deleted it is no longer searched.`,
  ],
  // schedules.js / settings2-pages.js / skills.js / tools.js
  [
    /^删除定时任务「(.+?)」？此操作不可恢复。$/,
    (m, n) => `Delete scheduled task "${n}"? This cannot be undone.`,
  ],
  [/^下次 (.+)$/, (m, w) => `Next ${w}`],
  [/^已触发「(.+?)」$/, (m, n) => `Triggered "${n}"`],
  [
    /^「(.+?)」没有触发（可能正在运行）$/,
    (m, n) => `"${n}" did not fire (it may already be running)`,
  ],
  [
    /^当前工作区不是 git 仓库(.*?)，任务会在工作区里直接执行——想用副本请先把工作区换到一个 git 仓库。$/,
    (m, ws) =>
      `The current workspace is not a git repository${ws}, so tasks run directly in the workspace — switch the workspace to a git repository to use copies.`,
  ],
  [
    /^([\s\S]+?)。继续会把里面的内容一起丢掉，且丢了找不回来。$/,
    (m, msg) =>
      `${i18nText(msg) || msg}. Continuing will discard its contents too, and once gone they cannot be recovered.`,
  ],
  [/^添加（(\d+)）$/, (m, n) => `Add (${n})`],
  [
    /^读取自 (.+?)（只解析 Host 名称）$/,
    (m, p) => `Read from ${p} (reads the Host name only)`,
  ],
  [
    /^在 (.+?) 写好 Host 后点「重新读取」。$/,
    (m, p) => `Write Host entries in ${p}, then click "Read again".`,
  ],
  [/^已记下 (\d+) 台主机$/, (m, n) => `Recorded ${n} hosts`],
  [
    /^将永久删除 (\d+) 条已结束任务的记录（盘上的归档文件一并删除），无法恢复。$/,
    (m, n) =>
      `This permanently deletes ${n} finished-task records (their archive files on disk go too) and cannot be undone.`,
  ],
  [
    /^「(.+?)」的记录和归档文件会被永久删除。$/,
    (m, g) =>
      `The record for "${g}" and its archive file will be permanently deleted.`,
  ],
  [
    /^运行 (\d+) 次 · 成功 (\d+) · (\d+) 步([\s\S]*)$/,
    (m, a, b, c, rest) =>
      `Ran ${a} times · ${b} succeeded · ${c} steps${i18nTail(rest) || rest}`,
  ],
  [/^· 参数: (.+)$/, (m, x) => ` · params: ${x}`],
  [/^技能「(.+?)」已启用$/, (m, n) => `Skill "${n}" enabled`],
  [/^技能「(.+?)」已停用$/, (m, n) => `Skill "${n}" disabled`],
  [
    /^删除技能「(.+?)」？此操作不可恢复。$/,
    (m, n) => `Delete skill "${n}"? This cannot be undone.`,
  ],
  [/^运行技能「(.+?)」$/, (m, n) => `Run skill "${n}"`],
  [
    /^技能执行(成功|结束)（([\s\S]*)）$/,
    (m, st, s) =>
      `Skill run ${st === '成功' ? 'succeeded' : 'finished'} (${i18nText(s) || s})`,
  ],
  [/^保存技能「(.+?)」$/, (m, n) => `Save skill "${n}"`],
  [/^技能「(.+?)」已保存$/, (m, n) => `Skill "${n}" saved`],
  [
    /^工具 (.+?) 权限已更新（(.+?)）$/,
    (m, t, v) => `${i18nText(t) || t} permission updated (${i18nText(v) || v})`,
  ],
  [
    /^(内置默认)?（当前 (.+?)）$/,
    (m, pre, x) =>
      `${pre ? 'Built-in default ' : ''}(currently ${i18nText(x) || x})`,
  ],
  [/^工作区：(.+)$/, (m, p) => `Workspace: ${p}`],
  [
    /^(\d+) 个 index\.html 不是 Gleam 生成的，已略过$/,
    (m, n) => `${n} index.html files were not generated by Gleam — skipped`,
  ],
  // 会话用量行：`≈12.3k tokens · 4 次调用 · 6 次工具`
  [
    /^([\s\S]*?) tokens · (\d+) 次调用(?: · (\d+) 次工具)?$/,
    (m, tk, calls, tools) =>
      `${tk} tokens · ${calls} calls${tools ? ` · ${tools} tools` : ''}`,
  ],
  // 用量 tooltip 的「；」串：整串不命中时由 i18nText 的「，；」分段规则拆开，
  // 逐段落到下面这三条。
  [
    /^输入 (\d+) \/ 输出 (\d+) token$/,
    (m, i, o) => `Input ${i} / output ${o} tokens`,
  ],
  [
    /^缓存命中 (\d+)%（(.+?) tokens）$/,
    (m, p, t) => `Cache hit ${p}% (${t} tokens)`,
  ],
  [/^(\d+) 次重试$/, (m, n) => `${n} retries`],
  // 改动清单的表头片段与账号行
  [/^可还原 (\d+)\/(\d+)$/, (m, a, b) => `Restorable ${a}/${b}`],
  [/^([\s\S]+) · 云端账号$/, (m, h) => `${h} · Cloud account`],
  [/^已连接 SSH：(.+)$/, (m, h) => `Connected over SSH: ${h}`],
  [/^继续完善 (.+?)：$/, (m, p) => `Keep improving ${p}:`],
  [
    /^在工作区新建 sites\/(.+?)：([\s\S]*)单文件 HTML \+ CSS，不依赖外部资源，桌面和手机都能看。$/,
    (m, id, prompt) =>
      `Create sites/${id} in the workspace: ${prompt}Single-file HTML + CSS, no external resources, works on desktop and mobile.`,
  ],
];

const I18N_ATTRS = ['placeholder', 'title', 'aria-label'];
const i18nOrig = new WeakMap();
let i18nLang = 'zh';
let i18nApplying = false;

// 翻规则里的「尾巴」：尾巴是拼接留下的片段，可能带着空白，先按 i18nNode 的规矩
// 归一化（空白压成单空格、去首尾）再查，否则 `· 参数: x` 会因前导空格查不中。
function i18nTail(s) {
  return i18nText(String(s).replace(/\s+/g, ' ').trim(), false);
}

// 输入框里预填的内容（.value 不走 DOM 文本节点那条路）。英文模式下同样给英文；
// 中文模式原样返回，不做任何翻译。
function i18nValue(text) {
  const s = String(text);
  return i18nLang === 'en' ? i18nText(s) || s : s;
}

// 取一条文案的英文；没有就返回 null（表示"这句不翻"）。
// 规则可以返回 null 表示"这条不归我管"，后面更专的规则还有机会——最靠前的
// 那条「前缀查表」就是这样：它认不出前缀时会主动让位给排在其后的具体规则。
function i18nText(key, isAttr) {
  if (Object.prototype.hasOwnProperty.call(I18N_EN, key)) return I18N_EN[key];
  // 属性层也走插值规则：title="上下文窗口占用 41%（…）" 这类同样是界面自己拼的句子。
  for (const [re, fn] of I18N_PATTERNS) {
    const m = re.exec(key);
    if (!m) continue;
    const hit = fn(...m);
    if (hit != null) return hit;
  }
  // 运行期自己拼上去的句号：`bits.join('；') + '。'`（app.js 的站点/改动汇总行）。
  // 整句查不到就把句号摘下来再查一次，查到了补回一个英文句号——别因为一个标点漏翻一整句。
  const HAN = /[\u4e00-\u9fff\uf900-\ufaff]/;
  if (key.endsWith('。')) {
    const en = i18nText(key.slice(0, -1), isAttr);
    if (en != null) return /[.!?;,:]$/.test(en) ? en : en + '.';
  }
  // 分隔符串起来的汇总行（app.js 里 `bits.join('；')` / `bits.join('，')`）：先按分号切，
  // 每段各自再走一遍完整规则（可能命中整句 pattern，也可能继续按逗号细分），分隔符换成
  // 英文标点。**必须先分号后逗号**：句子内部本来就有逗号（`有 N 个空间…，切到…就能扫到`），
  // 先按逗号切会把整句打碎。带中文的段必须每段都翻得动，整串才动；一段翻不动就整句不翻,
  // 不半翻——运行期内容（路径、名字）不该被抠掉半截。纯 ASCII 段原样穿过。
  const splitJoin = (sep, en) => {
    const parts = key.split(sep);
    if (parts.length < 2) return null;
    let hit = false;
    const out = [];
    for (const p of parts) {
      if (!HAN.test(p)) {
        out.push(p);
        continue;
      }
      const e = i18nText(p, isAttr);
      if (e == null) return null;
      hit = true;
      out.push(e);
    }
    return hit ? out.join(en) : null;
  };
  const semi = splitJoin('；', '; ');
  if (semi != null) return semi;
  const comma = splitJoin('，', ', ');
  if (comma != null) return comma;
  return null;
}

function i18nNode(n) {
  const raw = i18nOrig.has(n) ? i18nOrig.get(n) : n.nodeValue;
  if (raw == null) return;
  const box = /^(\s*)([\s\S]*?)(\s*)$/.exec(raw);
  const body = box[2];
  if (!body) return;
  // 源码里的长文案会被格式化器折行，DOM 里就带上了换行与缩进，而词条记的是「一行」。
  // 查表前把内部空白压成单空格；命中后只替换正文那一段，首尾空白原样留着。
  const key = body.replace(/\s+/g, ' ');
  const en = key in I18N_EN ? I18N_EN[key] : i18nText(key, false);
  if (en == null) return;
  if (!i18nOrig.has(n)) i18nOrig.set(n, raw);
  const want = body === en ? raw : raw.replace(body, en);
  if (n.nodeValue !== want) n.nodeValue = want;
}

function i18nAttrs(el) {
  for (const a of I18N_ATTRS) {
    const cur = el.getAttribute(a);
    if (cur == null) continue;
    const store = 'i18n' + a.replace(/-./g, (m) => m[1].toUpperCase());
    const orig = el.dataset[store] != null ? el.dataset[store] : cur;
    const en = i18nText(orig, true);
    if (en == null) continue;
    el.dataset[store] = orig;
    if (cur !== en) el.setAttribute(a, en);
  }
}

function i18nRestoreAttrs(el) {
  for (const a of I18N_ATTRS) {
    const store = 'i18n' + a.replace(/-./g, (m) => m[1].toUpperCase());
    if (el.dataset[store] != null) el.setAttribute(a, el.dataset[store]);
  }
}

// 单个文本节点：英文写译文，中文写回原文（原文存在 i18nOrig 里，只存一次）。
function i18nOneNode(n, restore) {
  if (restore) {
    const raw = i18nOrig.get(n);
    if (raw != null && n.nodeValue !== raw) n.nodeValue = raw;
    return;
  }
  i18nNode(n);
}

// 翻译一棵子树。只在英文模式下写 DOM；中文模式靠 i18nOrig 还原。
function i18nWalk(root, restore) {
  if (!root) return;
  if (root.nodeType === Node.TEXT_NODE) {
    i18nOneNode(root, restore);
    return;
  }
  if (root.nodeType === Node.ELEMENT_NODE) {
    if (restore) i18nRestoreAttrs(root);
    else i18nAttrs(root);
    root
      .querySelectorAll('*')
      .forEach((el) => (restore ? i18nRestoreAttrs(el) : i18nAttrs(el)));
  }
  const walker = document.createTreeWalker(root, NodeFilter.SHOW_TEXT);
  let n;
  while ((n = walker.nextNode())) i18nOneNode(n, restore);
}

// 后渲染的内容：只在英文模式下观察，且写 DOM 时把观察回调挡掉（否则自己触发自己）。
const i18nObserver = new MutationObserver((records) => {
  if (i18nApplying || i18nLang !== 'en') return;
  i18nApplying = true;
  try {
    for (const r of records) {
      if (r.type === 'characterData') i18nOneNode(r.target, false);
      else if (r.type === 'attributes') i18nAttrs(r.target);
      else r.addedNodes.forEach((n) => i18nWalk(n, false));
    }
  } finally {
    i18nApplying = false;
  }
});
i18nObserver.observe(document.documentElement, {
  childList: true,
  subtree: true,
  characterData: true,
  attributes: true,
  attributeFilter: I18N_ATTRS,
});

function applyLang(lang) {
  const en = lang === 'en';
  i18nLang = en ? 'en' : 'zh';
  document.documentElement.setAttribute('lang', en ? 'en' : 'zh-CN');
  i18nApplying = true;
  try {
    i18nWalk(document.body, !en);
  } finally {
    i18nApplying = false;
  }
  // 后端按界面语言组句子的面板（/api/connections、/api/cues 的 lang 参数）
  // 得拿新语言的数据重取一遍：静态文案上面已经翻完，动态面板的字在响应里。
  window.dispatchEvent(
    new CustomEvent('langchange', { detail: { lang: i18nLang } }),
  );
}
