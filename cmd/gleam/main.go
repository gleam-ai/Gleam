package main

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"gleam/internal/agent"
	"gleam/internal/agent/geo"
	"gleam/internal/buildinfo"
	"gleam/internal/config"
	"gleam/internal/harness/auth"
	"gleam/internal/harness/conversation"
	"gleam/internal/harness/credentials"
	"gleam/internal/harness/daemon"
	"gleam/internal/harness/growth"
	"gleam/internal/harness/memory"
	"gleam/internal/harness/registry"
	"gleam/internal/harness/safety"
	"gleam/internal/harness/scheduler"
	"gleam/internal/harness/skill"
	"gleam/internal/harness/space"
	"gleam/internal/llm"
	"gleam/internal/server"
	"gleam/internal/tools/desktop"
	"gleam/internal/tools/file"
	gittools "gleam/internal/tools/git"
	"gleam/internal/tools/mcp"
	"gleam/internal/tools/prompt"
	"gleam/internal/tools/shell"
	"gleam/internal/tools/std"
	"gleam/internal/tools/web"
	"gleam/pkg/types"
)

var version = buildinfo.Version

func main() {
	args := os.Args[1:]
	cmd := "help"
	if len(args) > 0 {
		cmd = args[0]
		args = args[1:]
	} else if !hasConsole() {
		cmd = "app"
	}
	var err error
	switch cmd {
	case "app":
		err = cmdApp(args)
	case "serve":
		err = cmdServe(args)
	case "webui":
		err = cmdWebUI(args)
	case "desktop-sidecar":
		err = cmdDesktopSidecar(args)
	case "goal":
		err = cmdGoal(args)
	case "tools":
		err = cmdTools(args)
	case "rules":
		err = cmdRules(args)
	case "skills":
		err = cmdSkills(args)
	case "schedule":
		err = cmdSchedule(args)
	case "memory":
		err = cmdMemory(args)
	case "pending":
		err = cmdPending(args)
	case "doctor":
		err = cmdDoctor(args)
	case "replay":
		err = cmdReplay(args)
	case "eval":
		err = cmdEval(args)
	case "daemon":
		err = cmdDaemon(args)
	case "mcp-fake-server":
		err = runMCPFakeServer()
	case "version":
		fmt.Printf("gleam %s (go agent, local-first)\n", version)
	case "help", "-h", "--help":
		printUsage()
	default:
		fmt.Fprintf(os.Stderr, "未知命令 %q\n\n", cmd)
		printUsage()
		os.Exit(2)
	}
	if err != nil {
		if errors.Is(err, flag.ErrHelp) {
			os.Exit(0)
		}
		fmt.Fprintln(os.Stderr, "错误:", err)
		os.Exit(1)
	}
}

func printUsage() {
	fmt.Print(`Gleam（微光）—— 本地优先的桌面 AI 智能体

用法:
  gleam app      [flags]              桌面端应用模式：引擎 + 应用窗口（关闭窗口即退出）
  gleam serve    [flags]              启动 JSON-RPC stdio 服务（编辑器插件接入入口）
  gleam webui    [flags]              启动 Web UI 服务（默认 127.0.0.1:8787，不开窗口）
  gleam goal "目标" [flags]            在命令行执行一个目标
  gleam tools                          列出全部已注册工具
  gleam rules   [flags]                规则集回查：版本指纹、每条规则的适用范围、某场景实际生效的规则
  gleam skills list                    列出技能
  gleam skills show NAME               查看技能详情
  gleam schedule list                  列出定时任务
  gleam memory search QUERY            检索长期记忆
  gleam memory save TEXT               写入长期记忆
  gleam pending [--clear]              列出/清理「等待审批中」的任务（进程重启后仍可见）
  gleam daemon install|uninstall|start|stop|status|queue  守护进程管理（24/7 后台运行）
  gleam doctor [flags]                 就绪体检（九坑自检）
  gleam replay <taskID> [flags]        回放历史任务：当时的计划与逐步结果；--rerun 用原计划真实重跑
  gleam eval   [flags]                 提示词/行为回归评测（默认 select 深度，离线可跑）
  gleam version                        版本信息

Flags:
  --config FILE      配置文件（默认尝试 configs/config.yaml）
  --workspace DIR    工作区根目录（文件工具边界）
  --data-dir DIR     数据目录（默认 ~/.gleam）
  --mock-llm         使用内置 Mock 模型（离线自测）
  --mock-script FILE Mock 响应脚本 JSON
  --mode MODE        auto | plan_first | interactive
  --json             仅 gleam doctor / gleam eval / gleam rules：输出结构化 JSON 报告
  --strict           仅 gleam doctor / gleam eval：存在不合格项（或评测回归）时以非零退出
  --depth DEPTH      仅 gleam eval：select | plan | full（默认 select）
  --tier NAME        仅 gleam eval / gleam rules：模型档位（eval 里为模拟，rules 里为查询场景）
  --task-mode MODE   仅 gleam rules：work | code | chat（默认 work）
  --role ID          仅 gleam rules：角色 ID，配合 --task-mode/--tier 查询场景
  --cases FILE       仅 gleam eval：自定义用例集 JSON
  --layer LAYER      仅 gleam eval：只跑指定分层 smoke|regression|edge|adversarial|holdout
  --emit-case ID     仅 gleam eval：把失败任务转成用例草稿（badcase 回流，需 --data-dir）
  --save FILE        仅 gleam eval：把本次报告写成基线
  --baseline FILE    仅 gleam eval：与基线对比并报出回归
`)
}

type runtime struct {
	cfg      *config.Config
	client   llm.Client
	reg      *registry.Registry
	mem      *memory.Manager
	gate     *safety.Gate
	skills   *skill.Store
	sched    *scheduler.Scheduler
	agent    *agent.Agent
	cleanup  func()
	growth   *growth.Log
	startMCP func()
}

type runtimeOption func(*runtimeConfig)

type runtimeConfig struct {
	noDefaultWorkspace bool
	asyncMCP           bool
}

func WithNoDefaultWorkspace() runtimeOption {
	return func(rc *runtimeConfig) { rc.noDefaultWorkspace = true }
}

func WithAsyncMCP() runtimeOption {
	return func(rc *runtimeConfig) { rc.asyncMCP = true }
}

func buildRuntime(configPath, workspace, dataDir string, mockLLM bool, mockScript string, notifier agent.Notifier, opts ...runtimeOption) (*runtime, error) {
	var rc runtimeConfig
	for _, opt := range opts {
		opt(&rc)
	}
	if configPath == "" {
		if _, err := os.Stat("configs/config.yaml"); err == nil {
			configPath = "configs/config.yaml"
		}
	}
	cfg, err := config.Load(configPath)
	if err != nil {
		return nil, err
	}
	if workspace != "" {
		cfg.Workspace = workspace
	}
	if dataDir != "" {
		cfg.DataDir = dataDir
	}
	_ = config.LoadOverlay(cfg, filepath.Join(cfg.DataDir, config.OverlayFile))
	if cfg.Workspace == "" && !rc.noDefaultWorkspace {
		if cwd, err := os.Getwd(); err == nil {
			cfg.Workspace = cwd
		}
	}

	var wsRoots []string
	if cfg.Workspace != "" {
		wsRoots = []string{cfg.Workspace}
	}

	credStore, err := credentials.Open(cfg.DataDir)
	if err != nil {
		return nil, fmt.Errorf("打开本地凭证失败: %w", err)
	}

	if mockLLM {
		cfg.LLM.Provider = "mock"
	}
	if cfg.Safety.Mode == "" {
		cfg.Safety.Mode = "auto"
	}
	agent.DataDir(cfg)

	if err := llm.SetProxyMode(cfg.Network.ProxyMode, cfg.Network.ProxyURL); err != nil {
		fmt.Fprintf(os.Stderr, "[gleam] 代理设置没生效（%v），本次按跟随系统处理\n", err)
	}

	var client llm.Client
	if cfg.LLM.Provider == "mock" {
		m := llm.NewMock()
		if mockScript != "" {
			scripts, err := llm.LoadScripts(mockScript)
			if err != nil {
				return nil, err
			}
			m.Apply(scripts)
		}
		client = m
	} else {
		baseURL, model, protocol := llm.ResolveTarget(cfg.LLM.ProviderID, cfg.LLM.Plan, cfg.LLM.BaseURL, cfg.LLM.Model, cfg.LLM.Protocol)
		cfg.LLM.BaseURL, cfg.LLM.Model, cfg.LLM.Protocol = baseURL, model, protocol
		agent.MarkInjectedLLMKey(cfg, baseURL)
		key, keyErr := agent.BindLLMKey(cfg, credStore, baseURL, "", false)
		if keyErr != nil {
			return nil, fmt.Errorf("读取本地密钥失败: %w", keyErr)
		}
		client = llm.New(protocol, baseURL, key, model,
			cfg.LLM.Temperature, cfg.LLM.MaxTokens, cfg.LLM.TimeoutSecs)
	}

	mem, err := memory.Open(cfg.DataDir, cfg.Memory.ShortTermCap, cfg.Memory.VectorDim, cfg.Memory.MaxItems)
	if err != nil {
		return nil, fmt.Errorf("打开记忆失败: %w", err)
	}

	growthLog, err := growth.Open(cfg.DataDir)
	if err != nil {
		return nil, fmt.Errorf("打开成长日志失败: %w", err)
	}

	convoStore, err := conversation.Open(cfg.DataDir)
	if err != nil {
		return nil, fmt.Errorf("打开会话存储失败: %w", err)
	}

	spaceStore, err := space.Open(cfg.DataDir, cfg.Workspace)
	if err != nil {
		return nil, fmt.Errorf("打开空间存储失败: %w", err)
	}

	skills, err := skill.Open(filepath.Join(cfg.DataDir, "skills"))
	if err != nil {
		return nil, fmt.Errorf("打开技能库失败: %w", err)
	}

	reg := registry.New()
	fileTools := file.New(wsRoots...)
	fileTools.RegisterAll(reg)
	reg.MustRegister(shell.NewWithRoots(time.Duration(cfg.Agent.StepTimeoutSecs)*time.Second, wsRoots...))
	gitCfg := func() gittools.Config {
		return gittools.Config{
			BranchPrefix:       cfg.Git.BranchPrefix,
			ForcePush:          cfg.Git.ForcePush,
			CommitInstructions: cfg.Git.CommitInstructions,
			Roots:              wsRoots,
		}
	}
	reg.MustRegister(gittools.NewBranch(gitCfg))
	reg.MustRegister(gittools.NewCommit(gitCfg))
	reg.MustRegister(gittools.NewPush(gitCfg))
	webTool := web.New()
	webTool.AllowPrivate = cfg.Safety.AllowPrivateWeb
	reg.MustRegister(webTool)
	reg.MustRegister(desktop.NewClipboardRead())
	reg.MustRegister(desktop.NewClipboardWrite())
	reg.MustRegister(desktop.NewScreenshot())
	reg.MustRegister(desktop.NewNotify())
	reg.MustRegister(desktop.NewSnippets(cfg.DataDir))
	reg.MustRegister(std.NewReply())

	var sched *scheduler.Scheduler
	if cfg.Scheduler.Enabled {
		sched, err = scheduler.Open(filepath.Join(cfg.DataDir, "schedules.json"), nil)
		if err != nil {
			return nil, fmt.Errorf("打开调度器失败: %w", err)
		}
	}

	gate := safety.New(cfg.Safety.Mode, cfg.Safety.TrustedTools, cfg.Safety.TrustedPaths,
		wsRoots, time.Duration(cfg.Safety.ApprovalTimeoutSecs)*time.Second)
	gate.SetAuditPath(filepath.Join(cfg.DataDir, auditFile))
	llm.SetEgressHook(func(host string, nbytes int) { gate.RecordEgress("llm", host, nbytes) })
	webTool.OnEgress = func(host string, nbytes int) { gate.RecordEgress("web.fetch", host, nbytes) }
	mcp.SetRemoteEgressHook(func(host string, nbytes int) { gate.RecordEgress("mcp.remote", host, nbytes) })
	gate.SetPendingPath(filepath.Join(cfg.DataDir, pendingFile))
	if len(cfg.Safety.ToolPermissions) > 0 {
		overrides := map[string]types.Permission{}
		for name, perm := range cfg.Safety.ToolPermissions {
			overrides[name] = permFromStr(perm)
		}
		gate.SetToolPermissions(overrides)
	}

	a := agent.New(cfg, client, reg, mem, gate, skills, sched, notifier)
	a.MCP = agent.NewMCPManager()
	a.FileTools = fileTools
	a.Growth = growthLog
	a.RebuildFastClient()
	if geoStore, err := geo.Open(filepath.Join(cfg.DataDir, "geo_history.json")); err == nil {
		a.GEO = geoStore
	}
	a.Convos = convoStore
	a.Spaces = spaceStore
	a.Creds = credStore
	authMgr := auth.New(credStore)
	authMgr.OnEgress = func(host string, nbytes int) { gate.RecordEgress("cloud", host, nbytes) }
	a.Auth = authMgr

	reg.MustRegister(prompt.NewGenerate(func() llm.Client { return a.LLM }))
	reg.MustRegister(std.NewMemSave(&memAdapter{m: mem, scope: a.MemoryScope}))
	reg.MustRegister(std.NewMemSearch(&memAdapter{m: mem, scope: a.MemoryScope}))
	reg.MustRegister(std.NewMemDelete(&memAdapter{m: mem, scope: a.MemoryScope}))
	if sched != nil {
		reg.MustRegister(std.NewScheduleCreate(&schedAdapter{a: a}))
		reg.MustRegister(std.NewScheduleList(&schedAdapter{a: a}))
		reg.MustRegister(std.NewScheduleDelete(&schedAdapter{a: a}))
	}
	reg.MustRegister(std.NewSkillList(&skillAdapter{s: skills}))
	reg.MustRegister(std.NewSkillRun(&skillAdapter{s: skills}, a))
	reg.MustRegister(std.NewAgentDelegate(a))

	startMCP := func() {}
	connectMCP := func() {
		if len(cfg.MCP) > 0 {
			ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
			mcpClients := mcp.RegisterAll(ctx, reg, toMCPCfgs(cfg.MCP), func(f string, xs ...any) {
				fmt.Fprintf(os.Stderr, "[mcp] "+f+"\n", xs...)
			})
			cancel()
			a.MCP.AddAll(mcpClients)
		}
	}
	if rc.asyncMCP {
		startMCP = connectMCP
	} else {
		connectMCP()
	}

	cleanup := func() {
		a.MCP.CloseAll()
		if sched != nil {
			sched.Stop()
		}
		_ = mem.Close()
	}
	if sched != nil {
		sched.Start()
	}

	return &runtime{cfg: cfg, client: client, reg: reg, mem: mem, gate: gate, skills: skills, sched: sched, agent: a, cleanup: cleanup, growth: growthLog, startMCP: startMCP}, nil
}

var sysNotify = func(title, message string) error {
	if os.Getenv("GLEAM_NO_SYS_NOTIFY") != "" {
		fmt.Fprintf(os.Stderr, "[schedule] 系统通知（已禁用，仅打印）：%s — %s\n", title, message)
		return nil
	}
	return desktop.Notify(title, message)
}

func notifyScheduledDone(j scheduler.Job, res *types.GoalResult, sink agent.Notifier) {
	if res == nil {
		return
	}
	ev := types.TaskDoneEvent{
		TaskID: res.TaskID,
		Origin: fmt.Sprintf("定时任务「%s」", j.Name),
		Result: *res,
	}
	if !j.ShouldNotify(ev.Succeeded()) {
		fmt.Fprintf(os.Stderr, "[schedule] %s（按通知策略不弹系统通知）\n", ev.Line())
	} else if err := sysNotify(ev.Title(), ev.Line()); err != nil {
		fmt.Fprintf(os.Stderr, "[schedule] 系统通知未送达（%v）\n", err)
	}
	if sink != nil {
		sink.OnTaskDone(ev)
	}
}

var runGoalFor = func(a *agent.Agent, ctx context.Context, req types.GoalRequest) *types.GoalResult {
	return a.RunGoal(ctx, req)
}

func fireScheduledJob(rt *runtime, j scheduler.Job) {
	mode := j.Mode
	if mode == "" {
		mode = "auto"
	}
	fmt.Fprintf(os.Stderr, "[schedule] 触发任务 %q: %s\n", j.Name, types.Shorten(j.Goal, 60))
	res := runGoalFor(rt.agent, context.Background(), types.GoalRequest{Goal: j.Goal, Mode: mode})
	if err := agent.SaveTaskResult(rt.cfg.DataDir, res); err != nil {
		fmt.Fprintf(os.Stderr, "[schedule] 任务未存档：%v\n", err)
	}
	notifyScheduledDone(j, res, rt.agent.Notifier)
}

func bindSchedulerFire(rt *runtime) {
	if rt.sched == nil {
		return
	}
	rt.sched.SetFire(func(j scheduler.Job) { fireScheduledJob(rt, j) })
}

func cmdServe(args []string) error {
	fs := flag.NewFlagSet("serve", flag.ContinueOnError)
	configPath := fs.String("config", "", "配置文件路径")
	workspace := fs.String("workspace", "", "工作区根目录")
	dataDir := fs.String("data-dir", "", "数据目录")
	mockLLM := fs.Bool("mock-llm", false, "使用 Mock 模型")
	mockScript := fs.String("mock-script", "", "Mock 响应脚本 JSON 路径")
	if err := fs.Parse(args); err != nil {
		return err
	}
	rt, err := buildRuntime(*configPath, *workspace, *dataDir, *mockLLM, *mockScript, nil)
	if err != nil {
		return err
	}
	defer rt.cleanup()
	bindSchedulerFire(rt)
	fmt.Fprintf(os.Stderr, "[gleam] 服务已启动 (model=%s, tools=%d, mode=%s)——等待 JSON-RPC 输入\n",
		rt.agent.LLM.Name(), rt.reg.Len(), rt.cfg.Safety.Mode)
	return server.RunStdio(rt.agent)
}

func splitFlagArgs(args []string, fs *flag.FlagSet) (flagArgs, positionals []string) {
	for i := 0; i < len(args); i++ {
		a := args[i]
		if strings.HasPrefix(a, "-") && a != "-" {
			flagArgs = append(flagArgs, a)
			name := strings.TrimLeft(a, "-")
			if strings.Contains(name, "=") {
				continue
			}
			if f := fs.Lookup(name); f != nil {
				if bv, ok := f.Value.(interface{ IsBoolFlag() bool }); ok && bv.IsBoolFlag() {
					continue
				}
				if i+1 < len(args) {
					flagArgs = append(flagArgs, args[i+1])
					i++
				}
			}
			continue
		}
		positionals = append(positionals, a)
	}
	return flagArgs, positionals
}

func cmdGoal(args []string) error {
	fs := flag.NewFlagSet("goal", flag.ContinueOnError)
	configPath := fs.String("config", "", "配置文件路径")
	workspace := fs.String("workspace", "", "工作区根目录")
	dataDir := fs.String("data-dir", "", "数据目录")
	mockLLM := fs.Bool("mock-llm", false, "使用 Mock 模型")
	mockScript := fs.String("mock-script", "", "Mock 响应脚本 JSON 路径")
	mode := fs.String("mode", "auto", "执行模式 auto|plan_first|interactive")
	flagArgs, positionals := splitFlagArgs(args, fs)
	if err := fs.Parse(flagArgs); err != nil {
		return err
	}
	rest := append(fs.Args(), positionals...)
	if len(rest) == 0 {
		fs.PrintDefaults()
		return fmt.Errorf(`用法: gleam goal "目标" [--mode auto|plan_first|interactive]
      [--workspace DIR] [--data-dir DIR] [--config FILE] [--mock-llm] [--mock-script FILE]`)
	}
	goal := strings.Join(rest, " ")

	notifier := newConsoleNotifier()
	rt, err := buildRuntime(*configPath, *workspace, *dataDir, *mockLLM, *mockScript, notifier)
	if err != nil {
		return err
	}
	defer rt.cleanup()
	bindSchedulerFire(rt)

	result := rt.agent.RunGoal(context.Background(), types.GoalRequest{Goal: goal, Mode: *mode})
	if err := agent.SaveTaskResult(rt.cfg.DataDir, result); err != nil {
		fmt.Fprintf(os.Stderr, "[gleam] 任务未存档：%v\n", err)
	} else {
		fmt.Fprintf(os.Stderr, "[gleam] 任务已存档：gleam replay %s 可回放（trace_id %s）\n", result.TaskID, result.TraceID)
	}
	fmt.Println(result.Summary)
	if os.Getenv("GLEAM_DEBUG") != "" {
		for _, st := range result.Steps {
			fmt.Fprintf(os.Stderr, "  [%s] %s (%s) %s\n", st.Status, st.StepID, st.Tool, st.Error)
		}
	}
	switch result.Status {
	case types.GoalSuccess:
		if result.RetryOK() > 0 {
			fmt.Fprintf(os.Stderr, "[gleam] %d 个步骤经历重试后才成功（GLEAM_DEBUG 可查看明细）\n", result.RetryOK())
		}
		return nil
	case types.GoalPartial:
		fmt.Fprintf(os.Stderr, "[gleam] 部分完成（完成度 %d/100）\n", result.Score)
		printFailureBreakdown(result)
		return nil
	default:
		fmt.Fprintf(os.Stderr, "[gleam] %s: %s（完成度 %d/100）\n", result.Status, result.Error, result.Score)
		printFailureBreakdown(result)
		return fmt.Errorf("目标未完成")
	}
}

func printFailureBreakdown(result *types.GoalResult) {
	if len(result.FailureBreakdown) == 0 {
		return
	}
	kinds := make([]types.ErrorKind, 0, len(result.FailureBreakdown))
	for k := range result.FailureBreakdown {
		kinds = append(kinds, k)
	}
	sort.Slice(kinds, func(i, j int) bool { return kinds[i] < kinds[j] })
	parts := make([]string, 0, len(kinds))
	for _, k := range kinds {
		parts = append(parts, fmt.Sprintf("%s=%d", k, result.FailureBreakdown[k]))
	}
	fmt.Fprintf(os.Stderr, "[gleam] 失败归因：%s（trace_id %s）\n", strings.Join(parts, " "), result.TraceID)
}

func cmdTools(args []string) error {
	rt, err := buildRuntime("", "", "", false, "", agent.NopNotifier{})
	if err != nil {
		return err
	}
	defer rt.cleanup()
	for _, t := range rt.reg.List() {
		fmt.Printf("%-24s %-13s %s\n", t.Name(), t.Permission(), t.Description())
	}
	return nil
}

func cmdSkills(args []string) error {
	sub := "list"
	var rest []string
	if len(args) > 0 {
		sub = args[0]
		rest = args[1:]
	}
	rt, err := buildRuntime("", "", "", false, "", agent.NopNotifier{})
	if err != nil {
		return err
	}
	defer rt.cleanup()
	switch sub {
	case "list":
		for _, s := range rt.skills.List() {
			fmt.Printf("%-24s v%-2d 运行 %d 次（成功 %d） %s\n", s.Name, s.Version, s.Runs, s.Successes, s.Description)
		}
	case "show":
		if len(rest) == 0 {
			return fmt.Errorf("用法: gleam skills show NAME")
		}
		s, err := rt.skills.Get(rest[0])
		if err != nil {
			return err
		}
		b, _ := json.MarshalIndent(s, "", "  ")
		fmt.Println(string(b))
	default:
		return fmt.Errorf("未知子命令 %q", sub)
	}
	return nil
}

func cmdSchedule(args []string) error {
	rt, err := buildRuntime("", "", "", false, "", agent.NopNotifier{})
	if err != nil {
		return err
	}
	defer rt.cleanup()
	if rt.sched == nil {
		return nil
	}
	for _, j := range rt.sched.ListJobs() {
		next := "-"
		if j.NextRun != nil {
			next = j.NextRun.Format("01-02 15:04")
		}
		state := "启用"
		if !j.Enabled {
			state = "停用"
		}
		fmt.Printf("%-20s %-6s next=%s goal=%s\n", j.Name, state, next, types.Shorten(j.Goal, 40))
	}
	return nil
}

func cmdMemory(args []string) error {
	sub := "search"
	var rest []string
	if len(args) > 0 {
		sub = args[0]
		rest = args[1:]
	}
	rt, err := buildRuntime("", "", "", false, "", agent.NopNotifier{})
	if err != nil {
		return err
	}
	defer rt.cleanup()
	switch sub {
	case "save":
		if len(rest) == 0 {
			return fmt.Errorf("用法: gleam memory save TEXT")
		}
		id, err := rt.mem.Remember(strings.Join(rest, " "), nil)
		if err != nil {
			return err
		}
		fmt.Println("已保存:", id)
	case "search":
		if len(rest) == 0 {
			return fmt.Errorf("用法: gleam memory search QUERY")
		}
		for _, h := range rt.mem.Relevant(strings.Join(rest, " "), 5) {
			fmt.Printf("%.3f  %s  %s\n", h.Score, h.ID[:8], types.Shorten(h.Content, 100))
		}
	default:
		return fmt.Errorf("未知子命令 %q", sub)
	}
	return nil
}

func cmdDaemon(args []string) error {
	sub := "status"
	var rest []string
	if len(args) > 0 {
		sub = args[0]
		rest = args[1:]
	}

	dataDir := defaultDataDir()
	binPath, err := os.Executable()
	if err != nil {
		return fmt.Errorf("获取可执行文件路径失败：%w", err)
	}

	mgr := daemon.New("gleam", dataDir, binPath)

	switch sub {
	case "install":
		fmt.Println("正在注册 Gleam 守护进程...")
		if err := mgr.Install(); err != nil {
			return err
		}
		fmt.Println("✓ 守护进程已注册（开机自启）")
		fmt.Println("  使用 'gleam daemon start' 立即启动")

	case "uninstall":
		fmt.Println("正在卸载 Gleam 守护进程...")
		if err := mgr.Uninstall(); err != nil {
			return err
		}
		fmt.Println("✓ 守护进程已卸载")

	case "start":
		fmt.Println("正在启动 Gleam 守护进程...")
		if err := mgr.Start(); err != nil {
			return err
		}
		fmt.Println("✓ 守护进程已启动")

	case "stop":
		fmt.Println("正在停止 Gleam 守护进程...")
		if err := mgr.Stop(); err != nil {
			return err
		}
		fmt.Println("✓ 守护进程已停止")

	case "status":
		info, err := mgr.Status()
		if err != nil {
			return err
		}
		fmt.Printf("服务名称：%s\n", info.Name)
		fmt.Printf("状态：%s\n", info.State)
		fmt.Printf("开机自启：%v\n", info.Enabled)

	case "queue":
		queue, err := daemon.OpenQueue(dataDir)
		if err != nil {
			return err
		}
		defer queue.Close()

		stats, err := queue.GetStats()
		if err != nil {
			return err
		}

		fmt.Printf("任务队列统计：\n")
		fmt.Printf("  待执行：%d\n", stats.Pending)
		fmt.Printf("  运行中：%d\n", stats.Running)
		fmt.Printf("  已完成：%d\n", stats.Completed)
		fmt.Printf("  失败：%d\n", stats.Failed)
		fmt.Printf("  总计：%d\n", stats.Total)

		if len(rest) > 0 && rest[0] == "--pending" {
			fmt.Println("\n待执行任务：")
			tasks, err := queue.ListPending()
			if err != nil {
				return err
			}
			for _, t := range tasks {
				fmt.Printf("  %-20s %s %s\n", t.ID[:8], t.Mode, types.Shorten(t.Goal, 50))
			}
		}

	default:
		return fmt.Errorf("未知子命令 %q（可用：install/uninstall/start/stop/status/queue）", sub)
	}

	return nil
}

func defaultDataDir() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return ".gleam"
	}
	return filepath.Join(home, ".gleam")
}

type consoleNotifier struct {
	stdin *bufio.Reader
}

func newConsoleNotifier() *consoleNotifier { return &consoleNotifier{stdin: bufio.NewReader(os.Stdin)} }

func (c *consoleNotifier) OnProgress(ev types.ProgressEvent) {
	if ev.Kind == "llm" {
		fmt.Fprintf(os.Stderr, "\r[gleam %3d%%] %s", ev.Progress, types.Shorten(ev.Message, 100))
		return
	}
	fmt.Fprintf(os.Stderr, "\n[gleam %3d%%] %s\n", ev.Progress, ev.Message)
}

func (c *consoleNotifier) OnTool(ev types.ToolEvent) {
	if ev.Phase != types.ToolEnd {
		return
	}
	icon, ms := "✓", ""
	switch ev.Status {
	case types.ToolFailed:
		icon = "✗"
	case types.ToolSkipped:
		icon = "⏭"
	}
	if ev.DurationMS > 0 {
		ms = fmt.Sprintf(" · %dms", ev.DurationMS)
	}
	line := fmt.Sprintf("[gleam] %s %s%s", icon, types.Shorten(ev.Title, 80), ms)
	if ev.Error != "" {
		line += fmt.Sprintf("（%s）", types.Shorten(ev.Error, 80))
	}
	fmt.Fprintln(os.Stderr, line)
}

func (c *consoleNotifier) OnApproval(req types.ApprovalRequest) types.ApprovalResponse {
	fmt.Fprintf(os.Stderr, "\n⚠ 需要你的批准（风险: %s）%s\n", req.Risk, req.Reason)
	for _, line := range req.Plan {
		fmt.Fprintf(os.Stderr, "  - %s\n", line)
	}
	fmt.Fprintf(os.Stderr, "允许执行吗? [y/N]（%v 秒内未答复将自动拒绝）> ", int(gApprovalTimeout().Seconds()))
	ch := make(chan string, 1)
	go func() {
		line, err := c.stdin.ReadString('\n')
		if err != nil {
			ch <- ""
			return
		}
		ch <- strings.TrimSpace(line)
	}()
	select {
	case line := <-ch:
		if line == "y" || line == "Y" || line == "yes" {
			return types.ApprovalResponse{Approved: true}
		}
		return types.ApprovalResponse{Approved: false, Note: "用户拒绝"}
	case <-time.After(gApprovalTimeout()):
		fmt.Fprintln(os.Stderr, "\n（超时，自动拒绝）")
		return types.ApprovalResponse{Approved: false, Note: "审批超时"}
	}
}

func (c *consoleNotifier) OnSuggestion(taskID, text string) {
	fmt.Fprintf(os.Stderr, "\n💡 建议：%s\n", text)
}

func (c *consoleNotifier) OnSuggestSkill(taskID string, draft types.SkillDraft) {
	fmt.Fprintf(os.Stderr, "\n💡 本次任务可固化为技能 %q（含 %d 个步骤）。在 JSON-RPC 中响应 agent/suggest_skill 并调用 skills/save 即可保存。\n",
		draft.Name, len(draft.Steps))
}

func (c *consoleNotifier) OnTaskDone(ev types.TaskDoneEvent) {
	fmt.Fprintf(os.Stderr, "\n🔔 %s：%s\n", ev.Title(), ev.Line())
}

func gApprovalTimeout() time.Duration {
	if v := os.Getenv("GLEAM_APPROVAL_TIMEOUT_SEC"); v != "" {
		var n int
		if _, err := fmt.Sscanf(v, "%d", &n); err == nil && n > 0 {
			return time.Duration(n) * time.Second
		}
	}
	return 5 * time.Minute
}

type memAdapter struct {
	m     *memory.Manager
	scope func() string
}

func (a *memAdapter) scopeNow() string {
	if a.scope == nil {
		return ""
	}
	return a.scope()
}

func (a *memAdapter) Remember(content string, tags []string) (string, error) {
	return a.m.RememberScoped(content, "", a.scopeNow(), tags)
}
func (a *memAdapter) Search(query string, k int) []std.SearchHit {
	hits := a.m.RelevantScoped(query, k, a.scopeNow())
	out := make([]std.SearchHit, 0, len(hits))
	for _, h := range hits {
		out = append(out, std.SearchHit{ID: h.ID, Content: h.Content, Tags: h.Tags, Score: h.Score})
	}
	return out
}
func (a *memAdapter) Count() int { return a.m.Long.Count() }
func (a *memAdapter) SoftDelete(id string) bool {
	ok := a.m.Long.SoftDelete(id)
	if ok {
		_ = a.m.Long.Flush()
	}
	return ok
}

type schedAdapter struct{ a *agent.Agent }

func (s *schedAdapter) AddJob(name, cron string, intervalSec int, goal, mode string) (std.JobDef, error) {
	j, err := s.a.Sched.AddJob(name, cron, intervalSec, goal, mode)
	if err != nil {
		return std.JobDef{}, err
	}
	return toJobDef(j), nil
}

func (s *schedAdapter) AddJobDetailed(name, cron string, intervalSec int, goal, mode, whenText, scheduleText string) (std.JobDef, error) {
	j, err := s.a.Sched.AddJobDetailed(name, cron, intervalSec, goal, mode, whenText, scheduleText)
	if err != nil {
		return std.JobDef{}, err
	}
	return toJobDef(j), nil
}
func (s *schedAdapter) DeleteJob(name string) (bool, error) { return s.a.Sched.DeleteJob(name) }
func (s *schedAdapter) ListJobs() []std.JobDef {
	jobs := s.a.Sched.ListJobs()
	out := make([]std.JobDef, 0, len(jobs))
	for _, j := range jobs {
		out = append(out, toJobDef(j))
	}
	return out
}

func toJobDef(j scheduler.Job) std.JobDef {
	d := std.JobDef{
		Name: j.Name, Cron: j.Cron, IntervalSec: j.IntervalSec,
		Goal: j.Goal, Mode: j.Mode, Enabled: j.Enabled,
		WhenText: j.WhenText, ScheduleText: j.ScheduleText,
	}
	if j.NextRun != nil {
		d.NextRun = j.NextRun.Format(time.RFC3339)
	}
	if j.LastRun != nil {
		d.LastRun = j.LastRun.Format(time.RFC3339)
	}
	return d
}

type skillAdapter struct{ s *skill.Store }

func (a *skillAdapter) ListSummaries() []std.SkillSummary {
	list := a.s.List()
	out := make([]std.SkillSummary, 0, len(list))
	for _, s := range list {
		out = append(out, std.SkillSummary{Name: s.Name, Description: s.Description, Version: s.Version, Runs: s.Runs, Successes: s.Successes})
	}
	return out
}
func (a *skillAdapter) GetParams(name string) ([]string, error) { return a.s.GetParams(name) }

func permFromStr(s string) types.Permission {
	switch s {
	case "readonly":
		return types.PermissionReadOnly
	case "full_access":
		return types.PermissionFullAccess
	default:
		return types.PermissionUserApproved
	}
}

func toMCPCfgs(in []config.MCPServerConfig) []mcp.ServerConfig {
	out := make([]mcp.ServerConfig, 0, len(in))
	for _, s := range in {
		out = append(out, mcp.ServerConfig{Name: s.Name, Command: s.Command, Args: s.Args, Trust: s.Trust, Enabled: s.Enabled})
	}
	return out
}
