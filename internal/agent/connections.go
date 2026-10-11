// Package agent 里的这一管负责一件事：把"这台机器上有哪些边界"列成一张台账。
//
// 为什么不做成"连接器市场"：别的桌面 Agent 那一屏回答的是"我还能连什么"，
// 而 Gleam 早已把实体散在 MCP / 工具 / 技能三层里——再加一屏画廊只是给同一批事实找第二个家，
// 而且那个家的朝向是"劝你多装"。这一层反过来回答：**这条线通了之后，谁被允许动我的什么**。
package agent

import (
	"fmt"
	"strings"

	"gleam/internal/harness/safety"
	"gleam/internal/llm"
)

// egressKinds 出网落点清单，**这一份是 owner**。
//
// 闸门 scripts/check-egress-owner.py 拿它与全仓所有 RecordEgress 调用点的 kind 字面量
// 双向对账（读源码前会先剥掉注释，免得注释里提一句这个函数就凭空多出一个落点）：
// 新接一条出网路径忘了在这里交代 → 红；在这里列了一条代码里没人记的出网 → 也红。
// 之所以值得为它写一道闸门：本地优先的产品最坏的失效不是"连得少"，而是
// **"连了而没交代"**——那句话在界面上看起来和"没连"一模一样。
//
// 台账行的 kind 与本清单必须一致，这条由 TestConnections_EveryEgressKindHasARow 在运行时判
// （闸门只能读源码，读不出"这一行今天到底画没画出来"）。
var egressKinds = []string{"llm", "web.fetch", "cloud", "feedback", "update", "go.toolchain", "market.remote", "mcp.remote", "git.remote"}

// EgressKinds 出网落点清单（供测试与自述用；改这份的同时必须改台账行）。
func EgressKinds() []string { return append([]string(nil), egressKinds...) }

// ConnectionRow 台账上的一条边界。
//
// 字段全是**已存在的事实**：目标来自配置与注册表，读数来自门控留痕，状态来自运行时。
// 这里没有任何一个手写的"能力宣传语"——一张会自己长出新行的表才有资格说"这就是全部"。
// 文案字段由 bi 给出中英两份（见 bilingual.go）：口径（什么时候说这句话）归这里，
// 两种语言的说法也归这里——前端拿到的就是成品，不再查表、不再拼装。
type ConnectionRow struct {
	ID       string `json:"id"`
	Title    string `json:"title"`
	Kind     string `json:"kind"`     // out | in | local
	KindText string `json:"kindText"` // 出网 | 入网 | 本机（显示口径也归这里，前端不再各写一份映射）
	Target   string `json:"target"`   // 通到哪
	Status   string `json:"status"`   // 现在到底是不是通的
	Trigger  string `json:"trigger"`  // 谁能让它发生
	Leaves   string `json:"leaves"`   // 会离开本机（或被它触及）的东西
	Trace    string `json:"trace"`    // 留痕在哪、怎么回看
	Untraced bool   `json:"untraced"` // 关键动作没有留痕：界面必须把它渲染成警示，而不是沉默
	Off      string `json:"off"`      // 想关掉或收紧，动哪里
	Stats    string `json:"stats"`    // 实测读数（有留痕的出网行才有）
	Alert    string `json:"alert"`    // 这一行现在值得警惕的一件事
}

// ConnectionLedger 一张台账 + 它的覆盖范围。
type ConnectionLedger struct {
	Rows        []ConnectionRow `json:"rows"`
	Scope       string          `json:"scope"`
	EgressScope string          `json:"egressScope"`
}

// ConnectionView 从真实运行状态派生台账。完全只读：不发模型请求、不写文件、不联网探测。
//
// bindAddr 由接入层给（引擎不知道自己在监听哪个端口，这是宿主的事实）。
// 传空串表示"本机服务这一行拿不到监听地址"——那一行会如实说"未知"，不会假装是回环。
// en 表示界面语言：true 给英文说法，false（默认）给中文。每句文案的两份说法都在本文件里，
// 由这一个参数挑——前端只传语言，不翻句子。
func (a *Agent) ConnectionView(bindAddr string, en bool) ConnectionLedger {
	rep := a.Gate.EgressStats()
	scope := egressScopeText(rep, a.Gate.AuditPath(), en)

	rows := []ConnectionRow{a.llmRow(rep, en), a.webFetchRow(rep, en)}
	if in := inboundRow(bindAddr, en); in != nil {
		rows = append(rows, *in)
	}
	rows = append(rows, a.mcpRows(en)...)
	rows = append(rows, a.cloudRow(rep, en), a.feedbackRow(rep, en), updateRow(rep, en),
		goToolchainRow(rep, en), marketRemoteRow(rep, en), mcpRemoteRow(rep, en),
		a.gitRemoteRow(rep, en), marketRow(en))

	return ConnectionLedger{
		Rows: rows,
		Scope: bi{
			"这张表列的是「常驻边界」：配置里就存在、随时可能被动用的那几处。" +
				"登录时临时开的本机回环端口（几分钟后关闭）不在这里。",
			"This table lists the standing boundaries: the ones that exist in configuration and can " +
				"be put to use at any time. Temporary loopback ports opened at sign-in (closed again " +
				"after a few minutes) are not listed here.",
		}.pick(en),
		EgressScope: scope,
	}
}

// egressScopeText 读数覆盖范围必须跟着读数走。
//
// 内存环只有最近若干条、重启即清零。界面若只印"N 次 / M 字节"，读者会理解成
// "这台机器上一共发出去多少"——那是把"我不知道"讲成答案（同 §4.6.33 那一类错）。
// 环被写满时（Scanned == Cap）说法必须变，且容量这个数字不许前端自己记。
//
// 第二层：指路只能指向真实存在的东西。没开审计落盘时让人去 `pending --audit`，
// 他查到的"没有记录"会被读成"确实没出过网"，而事实是"这台机器压根没记"。
func egressScopeText(rep safety.EgressReport, auditPath string, en bool) string {
	back := bi{
		"更早的记录在落盘审计里，`gleam pending --audit --egress` 可只看这一类",
		"earlier records live in the on-disk audit, where `gleam pending --audit --egress` shows this kind alone",
	}.pick(en)
	if auditPath == "" {
		back = bi{
			"这个构建没开审计落盘，内存环之外的事没有记录",
			"this build has no on-disk audit, so anything beyond the in-memory ring is unrecorded",
		}.pick(en)
	}
	switch {
	case rep.Scanned == 0:
		return bi{
			"内存里还没有任何出网留痕；",
			"The in-memory ring holds no egress records yet; ",
		}.pick(en) + back + bi{"。", "."}.pick(en)
	case rep.Scanned >= rep.Cap:
		return fmt.Sprintf(bi{
			"下面的读数只数了内存里最近 %d 条留痕（环已写满，更早的已被挤掉；重启清零；%s）",
			"The readings below cover only the most recent %d records held in memory (the ring is full, older ones were evicted; cleared on restart; %s)",
		}.pick(en), rep.Scanned, back)
	default:
		return fmt.Sprintf(bi{
			"下面的读数来自本次运行内存里的 %d 条留痕（重启清零；%s）",
			"The readings below come from %d in-memory records from this run (cleared on restart; %s)",
		}.pick(en), rep.Scanned, back)
	}
}

// egressStat 取某一类出网落点的实测读数。
func egressStat(rep safety.EgressReport, kind string) (safety.EgressStat, bool) {
	for _, s := range rep.Stats {
		if s.Kind == kind {
			return s, true
		}
	}
	return safety.EgressStat{}, false
}

// egressStatsText 一行出网读数的说法。没有记录要说成"没有记录"，不能留空——
// 空白格子在界面上的读法是"这行不归我管"，而事实是"这台机器至今没往它发过东西"。
func egressStatsText(rep safety.EgressReport, kind string, en bool) string {
	st, ok := egressStat(rep, kind)
	if !ok {
		return bi{
			"本次运行还没有这一类的出网记录",
			"No egress records of this kind in this run yet",
		}.pick(en)
	}
	hosts := strings.Join(st.Hosts, bi{"、", ", "}.pick(en))
	if len(st.Hosts) == 0 {
		hosts = bi{"主机名未知", "host names unknown"}.pick(en)
	}
	return fmt.Sprintf(bi{
		"已出网 %d 次，涉及 %d 个主机（%s），累计 %s",
		"%d egress calls, %d host(s) (%s), %s in total",
	}.pick(en), st.Count, len(st.Hosts), hosts, humanBytes(st.Bytes, en))
}

// humanBytes 读数要说人话；这是显示口径，归后端一处（前端再写一份就会有两个"1.2 KB"）。
func humanBytes(n int, en bool) string {
	byteWord := bi{"字节", "B"}.pick(en)
	switch {
	case n < 1024:
		return fmt.Sprintf("%d %s", n, byteWord)
	case n < 1024*1024:
		return fmt.Sprintf("%.1f KB", float64(n)/1024)
	default:
		return fmt.Sprintf("%.1f MB", float64(n)/1024/1024)
	}
}

// llmRow 模型服务这一行：本机唯一会把**提示词全文**送出去的边界。
//
// 目标主机取 `llm.KeyScope(cfg.LLM.BaseURL)`——那正是密钥绑定的那把尺（见 §4.6.26），
// 台账与它读同一个数，才不会出现"密钥说是 A、台账写着 B"。
func (a *Agent) llmRow(rep safety.EgressReport, en bool) ConnectionRow {
	base := strings.TrimSpace(a.Cfg.LLM.BaseURL)
	host := llm.KeyScope(base)
	r := ConnectionRow{
		ID: "llm", Title: bi{"模型服务", "Model service"}.pick(en), Kind: "out",
		KindText: bi{"出网", "Egress"}.pick(en),
		Trigger: bi{
			"每一次规划、执行、反思、验收，以及对话模式下每一条回答",
			"Every planning, execution, reflection and review call, plus every chat-mode reply",
		}.pick(en),
		Leaves: bi{
			"提示词全文：目标、系统提示、工作目录路径、会话历史、工具输出片段",
			"Full prompts: the goal, the system prompt, the working-directory path, session history, tool output snippets",
		}.pick(en),
		Trace: bi{
			"门控留痕 action=egress、kind=llm（只记主机与字节，不记正文）",
			"Gate record action=egress, kind=llm (hosts and bytes only, never body text)",
		}.pick(en),
		Off: bi{
			"设置 → 模型：换成模拟模型（provider: mock）就不再发包；密钥只存本机 credentials.json，且只允许发往绑定过的那一个接入主机",
			"Settings → Model: switch to the mock model (provider: mock) and no packets leave; keys stay in the local credentials.json and may only be sent to the one endpoint host they are bound to",
		}.pick(en),
		Stats: egressStatsText(rep, "llm", en),
	}
	if a.Cfg.LLM.Provider == "mock" || host == "" {
		r.Status = bi{
			"没配真实模型，这条线发不出东西",
			"No real model configured — this line carries nothing",
		}.pick(en)
		r.Target = "—"
		r.Leaves = bi{
			"不外发：当前用的是模拟模型/未填接入地址",
			"Nothing leaves: a mock model is in use, or no endpoint address is set",
		}.pick(en)
		r.Alert = bi{
			"没有模型接入，界面里的智能功能都不会真的发包",
			"No model endpoint — none of the smart features in the UI send real requests",
		}.pick(en)
		return r
	}
	r.Status = bi{"开着", "On"}.pick(en)
	r.Target = host
	if st, ok := egressStat(rep, "llm"); ok {
		for _, h := range st.Hosts {
			if h != host {
				r.Alert = fmt.Sprintf(bi{
					"留痕里出现过与当前接入不同的主机 %s：那说明发包时的接入地址与设置页现在写的不是同一个",
					"Records show host %s, which differs from the current endpoint — the address used when sending is not the one the settings page shows now",
				}.pick(en), h)
				break
			}
		}
	}
	return r
}

// webFetchRow 网页抓取：这是**唯一一处目标不由用户配置**的出网边界——
// 网址是模型自己挑的，所以这一行的重点在"自动放行"这件事本身。
func (a *Agent) webFetchRow(rep safety.EgressReport, en bool) ConnectionRow {
	r := ConnectionRow{
		ID: "web.fetch", Title: bi{"网页抓取", "Web fetch"}.pick(en), Kind: "out",
		KindText: bi{"出网", "Egress"}.pick(en),
		Target: bi{
			"任意公网主机（网址由模型在任务里自己挑）",
			"Any public host (URLs are chosen by the model during the task)",
		}.pick(en),
		Trigger: bi{
			"模型判断需要看某个网页时——它是只读工具，默认不经过你的批准",
			"Whenever the model decides it needs a page — a read-only tool, no approval from you by default",
		}.pick(en),
		Leaves: bi{
			"你要它看的那个网址（含查询串）；抓回来的正文留在本机，不进审计",
			"The URL you asked it to read (query string included); fetched content stays local and is not audited",
		}.pick(en),
		Trace: bi{
			"门控留痕 action=egress、kind=web.fetch（记主机与网址长度）",
			"Gate record action=egress, kind=web.fetch (host and URL length)",
		}.pick(en),
		Stats: egressStatsText(rep, "web.fetch", en),
		Off: bi{
			"设置 → 工具 → web.fetch 改「需我批准」，让每一次抓取都经过你点头；彻底摘掉它要改代码，目前没有单独开关",
			"Settings → Tools → web.fetch → \"Ask first\", so every fetch waits for your nod; removing it entirely takes a code change — there is no separate switch today",
		}.pick(en),
	}
	if t, ok := a.Reg.Get("web.fetch"); ok {
		perm := a.Gate.EffectivePermission(t)
		if perm.String() == "readonly" {
			r.Status = bi{"开着（自动放行）", "On (auto-allowed)"}.pick(en)
		} else {
			r.Status = bi{"开着（每次问你）", "On (asks every time)"}.pick(en)
		}
	} else {
		r.Status = bi{"这个构建里没注册", "Not registered in this build"}.pick(en)
		r.Target = "—"
	}
	if a.Cfg.Safety.AllowPrivateWeb {
		r.Alert = bi{
			"已打开 safety.allow_private_web：抓取可以打到本机与内网地址（含云元数据接口）——被注入的提示词就多了一条读内网的路",
			"safety.allow_private_web is on: fetches can reach loopback and private-network addresses (cloud metadata endpoints included) — an injected prompt gains one more route into your internal network",
		}.pick(en)
	}
	return r
}

// inboundRow 本机服务端口：台账里唯一一条"入"的方向。
//
// 这一行值得单独存在，是因为它常被当成"这是本机所以安全"。/api 现在要每次启动随机生成的口令
// （internal/webui/guard.go），还校验 Host 与跨站来源，浏览器里的网页替你发请求这条路已经堵上；
// 但口令文件就在数据目录里，**同一用户下的本机进程读得到它**，拿到口令就能提交目标、裁决审批、读你的会话。
// 默认只绑回环；**风险在有人手改 --addr 之后变大**（明文 HTTP，口令在网络上可见），所以状态必须如实印出来。
func inboundRow(bindAddr string, en bool) *ConnectionRow {
	if strings.TrimSpace(bindAddr) == "" {
		return nil
	}
	loopback := isLoopbackAddr(bindAddr)
	r := ConnectionRow{
		ID: "inbound", Title: bi{"本机服务端口", "Local service port"}.pick(en),
		Kind: "in", KindText: bi{"入网", "Inbound"}.pick(en),
		Target: bindAddr,
		Status: bi{"在监听", "Listening"}.pick(en),
		Trigger: bi{
			"能连到这个端口、并持有本次启动口令的程序",
			"Any program that can reach this port and holds this launch's token",
		}.pick(en),
		Leaves: bi{
			"不外发。但持有口令的一方能提交目标、批准或拒绝审批、读你的会话与设置",
			"Nothing leaves. But whoever holds the token can submit goals, approve or reject approvals, and read your sessions and settings",
		}.pick(en),
		Trace: bi{
			"没有留痕：这个端口不记录谁连过",
			"No records: this port does not log who connected",
		}.pick(en),
		Untraced: true,
		Off: bi{
			"桌面端固定只绑 127.0.0.1；命令行别带 --addr，或显式写 127.0.0.1:8787",
			"The desktop build always binds 127.0.0.1; on the command line, drop --addr or write 127.0.0.1:8787 explicitly",
		}.pick(en),
	}
	if loopback {
		r.Alert = bi{
			"回环地址：只有本机能连，/api 要本次启动的口令。口令文件在数据目录里，同一用户下的本机进程读得到它",
			"Loopback address: only this machine can connect, and /api requires this launch's token. The token file sits in the data directory — local processes under the same user can read it",
		}.pick(en)
	} else {
		r.Status = bi{"在监听（非回环）", "Listening (non-loopback)"}.pick(en)
		r.Alert = bi{
			"监听在非回环地址：局域网里的设备能连上来。/api 要口令，但连接是明文 HTTP，口令在网络上可见。要出这台机器，请放到带 TLS 的反代后面",
			"Listening on a non-loopback address: devices on the local network can connect. /api requires a token, but the connection is plain HTTP and the token is visible on the wire. To go beyond this machine, put it behind a TLS-terminating reverse proxy",
		}.pick(en)
	}
	return &r
}

// isLoopbackAddr 只看地址前缀：回环的判定归这里，不引 net 包做多一次解析——
// 台账这一层的职责是"如实交代"，把 net 的判定借来反而多一处会漂的地方。
func isLoopbackAddr(addr string) bool {
	a := strings.TrimSpace(addr)
	host := a
	if i := strings.LastIndexByte(a, ':'); i >= 0 {
		host = a[:i]
	}
	host = strings.Trim(host, "[]")
	return host == "127.0.0.1" || host == "localhost" || host == "::1" || strings.HasPrefix(host, "127.")
}

// mcpRows 一台 MCP 服务器一行。
//
// **为什么它不是"连接器"**：MCP 在 Gleam 里不通网络，而是**在你机器上起一个进程**。
// 于是它的爆炸半径不是"我能不能访问某个云"，而是"那个进程拿你的文件当自己的文件"。
// 这一层的留痕只到"谁被调用、批没批"，进程自己又连了什么，本机不知道——所以 Untraced 必须为真。
func (a *Agent) mcpRows(en bool) []ConnectionRow {
	out := []ConnectionRow{}
	for _, m := range a.MCPList() {
		name, _ := m["name"].(string)
		command, _ := m["command"].(string)
		enabled, _ := m["enabled"].(bool)
		connected, _ := m["connected"].(bool)
		tools, _ := m["tools"].(int)
		status := bi{"已连接", "Connected"}.pick(en)
		if !enabled {
			status = bi{"已停用：现在起不来进程", "Disabled: no process will be started"}.pick(en)
		} else if !connected {
			status = bi{"配置在，但这次没连上", "Configured, but not connected this run"}.pick(en)
		}
		out = append(out, ConnectionRow{
			ID:       "mcp." + name,
			Title:    bi{"MCP 服务器 ", "MCP server "}.pick(en) + name,
			Kind:     "local",
			KindText: bi{"本机", "Local"}.pick(en),
			Target:   bi{"本机进程 ", "local process "}.pick(en) + command,
			Status:   status,
			Trigger: fmt.Sprintf(bi{
				"模型调用它注册的 %d 个工具时（工具名前缀 mcp.%s.）",
				"When the model calls any of its %d tools (tool-name prefix mcp.%s.)",
			}.pick(en), tools, name),
			Leaves: bi{
				"不外发到这里；但它是一个跑在你机器上的进程，能读写什么由它自己决定",
				"Nothing leaves through here; but it is a process running on your machine, and what it may read or write is its own call",
			}.pick(en),
			Trace: bi{
				"调用与审批有留痕；这个进程随后又连了什么、动了什么文件，本机没有留痕",
				"Calls and approvals are recorded; whatever this process connects to or touches afterwards leaves no local trace",
			}.pick(en),
			Untraced: true,
			Off: bi{
				"设置 → MCP：停用（保留配置）或卸载（连配置一起删）。它的权限级别也在那一页",
				"Settings → MCP: disable (keep the config) or uninstall (remove the config too). Its permission level lives on that page as well",
			}.pick(en),
		})
	}
	return out
}

// cloudRow 云端账号：只有配了项目地址才存在，所以这一行会说"没配"。
func (a *Agent) cloudRow(rep safety.EgressReport, en bool) ConnectionRow {
	r := ConnectionRow{
		ID: "cloud", Title: bi{"云端账号", "Cloud account"}.pick(en), Kind: "out",
		KindText: bi{"出网", "Egress"}.pick(en),
		Trigger: bi{
			"你在「我的 → 账号」点注册/登录/登出那一下，以及令牌临近过期时的续期",
			"When you click sign up / sign in / sign out under Me → Account, plus token renewal as expiry approaches",
		}.pick(en),
		Leaves: bi{
			"邮箱、密码或一次性授权码、会话令牌；发到你自己填的 Supabase 项目",
			"Email, password or one-time auth code, session tokens — sent to the Supabase project you entered yourself",
		}.pick(en),
		Trace: bi{
			"门控留痕 action=egress、kind=cloud",
			"Gate record action=egress, kind=cloud",
		}.pick(en),
		Stats: egressStatsText(rep, "cloud", en),
		Off: bi{
			"「我的 → 账号」退出登录清掉会话；把项目地址填掉就彻底断开",
			"Me → Account: sign out to clear the session; clear the project URL to disconnect for good",
		}.pick(en),
	}
	if a.Creds == nil {
		r.Status = bi{
			"凭证存储没装配，发不出去",
			"Credential store not wired up — nothing can be sent",
		}.pick(en)
		r.Target = "—"
		return r
	}
	c := a.Creds.GetCloudConfig()
	if c == nil || strings.TrimSpace(c.SupabaseURL) == "" {
		r.Status = bi{"未配置：这条线不存在", "Not configured: this line does not exist"}.pick(en)
		r.Target = "—"
		r.Leaves = bi{
			"不外发：登录功能没被启用，也没有可发的东西",
			"Nothing leaves: sign-in is not enabled and there is nothing to send",
		}.pick(en)
		return r
	}
	r.Status = bi{"已配置", "Configured"}.pick(en)
	r.Target = llm.KeyScope(c.SupabaseURL)
	return r
}

// feedbackRow 反馈投递：用户交出去的是他写的那段话，所以这一行必须写清发出去前动了什么。
func (a *Agent) feedbackRow(rep safety.EgressReport, en bool) ConnectionRow {
	r := ConnectionRow{
		ID: "feedback", Title: bi{"反馈投递", "Feedback delivery"}.pick(en), Kind: "out",
		KindText: bi{"出网", "Egress"}.pick(en),
		Trigger: bi{
			"只有你点「提交反馈」那一下",
			"Only when you click \"Submit feedback\"",
		}.pick(en),
		Leaves: bi{
			"你写的正文（发送前把本机密钥与绝对路径挖掉）加一份上下文快照；本地归档永远先落住",
			"The text you wrote (local secrets and absolute paths scrubbed before sending) plus a context snapshot; the local archive is always written first",
		}.pick(en),
		Trace: bi{
			"门控留痕 action=egress、kind=feedback",
			"Gate record action=egress, kind=feedback",
		}.pick(en),
		Stats: egressStatsText(rep, "feedback", en),
		Off: bi{
			"不配云端项目地址就只落本地（默认就是这样），一条都不会发出去",
			"Without a cloud project URL everything stays local (the default) — not one message is ever sent",
		}.pick(en),
	}
	if a.Creds == nil {
		r.Status = bi{
			"凭证存储没装配，发不出去",
			"Credential store not wired up — nothing can be sent",
		}.pick(en)
		r.Target = "—"
		return r
	}
	c := a.Creds.GetCloudConfig()
	// 地址与 key 两件都齐才算配好投递：只有地址的请求一定 401，
	// 那种时候这一行说"已配置"就是骗人（判定与 feedback.Configured 同一把尺）。
	if strings.TrimSpace(c.SupabaseURL) == "" || strings.TrimSpace(c.SupabaseAnonKey) == "" {
		r.Status = bi{
			"未配置：反馈只落本机",
			"Not configured: feedback stays on this machine",
		}.pick(en)
		r.Target = "—"
		r.Leaves = bi{
			"不外发：反馈与它的上下文快照都留在数据目录里",
			"Nothing leaves: the feedback and its context snapshot both stay in the data directory",
		}.pick(en)
		return r
	}
	r.Status = bi{"已配置", "Configured"}.pick(en)
	r.Target = llm.KeyScope(c.SupabaseURL)
	return r
}

// updateRow 自我更新：这条线只在用户点了「更新」那一下才通。
//
// 它和「市场」那一行正好相反：市场是"你以为是连接、其实不是"，更新是
// "你以为是本机操作、其实在往 GitHub 拉一个可执行文件"。自替换是这台机器上
// 动作最大的一次出网，所以它必须有一行，且必须写清下载地址只认官方 release。
func updateRow(rep safety.EgressReport, en bool) ConnectionRow {
	return ConnectionRow{
		ID: "update", Title: bi{"自我更新", "Self-update"}.pick(en), Kind: "out",
		KindText: bi{"出网", "Egress"}.pick(en),
		Target: bi{
			"GitHub 官方 release（下载地址只认官方仓库的资产，前端递不进别的来源）",
			"Official GitHub releases (only assets of the official repository are accepted; the UI cannot hand in any other source)",
		}.pick(en),
		Status: bi{
			"待命：只有你点「检查更新 → 更新」才下载",
			"Standing by: downloads only when you click \"Check for updates → Update\"",
		}.pick(en),
		Trigger: bi{
			"「我的 / 关于」里点「检查更新」，再点「下载并替换」那一下",
			"Me / About: click \"Check for updates\", then \"Download and replace\"",
		}.pick(en),
		Leaves: bi{
			"不外发你的数据；这一下只往外取一个新版可执行文件，随后替换本机程序",
			"None of your data leaves; this only pulls a new executable, then replaces the local program",
		}.pick(en),
		Trace: bi{
			"门控留痕 action=egress、kind=update（记主机与字节）。替换前旧程序留成 .old",
			"Gate record action=egress, kind=update (host and bytes). The previous binary is kept as .old before the swap",
		}.pick(en),
		Stats: egressStatsText(rep, "update", en),
		Off: bi{
			"不点它就不发任何包；只想看有没有新版就点「检查更新」，那一步只发一次 GET",
			"Click nothing and nothing is sent; to just see whether a new version exists, \"Check for updates\" alone sends a single GET",
		}.pick(en),
	}
}

// goToolchainRow Go 工具链安装：这条线只在设置页点「安装 Go」那一下才通。
//
// 为什么它必须进台账：这是一条**用户不会想到的**出网路径——他以为在装开发工具，
// 机器却在往镜像站拉一个 80MB 的压缩包。落的落点、装到哪里、怎么校验，都要写明白。
// 目标主机写"镜像站"而不是某一个域名：按顺序试阿里云、官方国内镜像、官方站，
// 真发了包的那一个由留痕读数回答（这一行有 Stats）。
func goToolchainRow(rep safety.EgressReport, en bool) ConnectionRow {
	return ConnectionRow{
		ID: "go.toolchain", Title: bi{"Go 工具链安装", "Go toolchain install"}.pick(en), Kind: "out",
		KindText: bi{"出网", "Egress"}.pick(en),
		Target: bi{
			"Go 官方下载镜像（按序试阿里云 / 官方国内镜像 / 官方站）",
			"Official Go download mirrors (tried in order: Alibaba Cloud, the official China mirror, then the official site)",
		}.pick(en),
		Status: bi{
			"待命：只有你点「安装 Go」才下载",
			"Standing by: downloads only when you click \"Install Go\"",
		}.pick(en),
		Trigger: bi{
			"设置 → 电脑 → 未检测到 Go 时点「安装 Go」那一下",
			"Settings → Machine: clicking \"Install Go\" when no Go is detected",
		}.pick(en),
		Leaves: bi{
			"不外发你的数据；这一下只往外取一个 Go 归档，装进 <数据目录>/tools/go",
			"None of your data leaves; this only pulls one Go archive, installed into <data dir>/tools/go",
		}.pick(en),
		Trace: bi{
			"门控留痕 action=egress、kind=go.toolchain（记主机与字节）",
			"Gate record action=egress, kind=go.toolchain (host and bytes)",
		}.pick(en),
		Stats: egressStatsText(rep, "go.toolchain", en),
		Off: bi{
			"不点它就不发任何包；已经装好的 Go 不会被重新拉取（检测到即跳过）",
			"Click nothing and nothing is sent; an already-installed Go is never pulled again (detection skips it)",
		}.pick(en),
	}
}

// marketRemoteRow 市场远端目录：查 MCP 官方注册表这条路。
//
// 为什么它必须单独有一行：台账下面那行（market）说的是"内置目录一次请求都不发"，
// 那句话在接了远端目录之后只对内置那一半成立。把两条分开列，读者才不会读到一句
// 已经被自己推翻了一半的话——含混的对账比不做对账更坏。
func marketRemoteRow(rep safety.EgressReport, en bool) ConnectionRow {
	return ConnectionRow{
		ID: "market.remote", Title: bi{"市场远端目录", "Market remote catalog"}.pick(en), Kind: "out",
		KindText: bi{"出网", "Egress"}.pick(en),
		Target: bi{
			"MCP 官方注册表（registry.modelcontextprotocol.io）；npm 源只用于测速与安装时指定 registry",
			"The official MCP registry (registry.modelcontextprotocol.io); npm registries are only used for latency probes and install-time registry choice",
		}.pick(en),
		Status: bi{
			"开着：打开市场页或搜索时按需拉取，结果缓存 10 分钟",
			"On: fetched on demand when the market page opens or you search; results are cached for 10 minutes",
		}.pick(en),
		Trigger: bi{
			"你打开市场页 / 搜索目录时（自动），或点「刷新目录」时（强制）",
			"Opening the market page / searching the catalog (automatic), or clicking \"Refresh catalog\" (forced)",
		}.pick(en),
		Leaves: bi{
			"不外发你的数据；只发一个搜索词做目录查询，查询串本身不记入留痕",
			"None of your data leaves; only a search term goes out for the catalog query, and the term itself is not recorded",
		}.pick(en),
		Trace: bi{
			"门控留痕 action=egress、kind=market.remote（只记主机与字节）",
			"Gate record action=egress, kind=market.remote (hosts and bytes only)",
		}.pick(en),
		Stats: egressStatsText(rep, "market.remote", en),
		Off: bi{
			"不打开市场页就不发；目录连不上时自动退回内置目录与上次缓存，不影响已装好的 MCP",
			"Nothing is sent unless you open the market page; if the catalog is unreachable it falls back to the built-in list and the last cache — installed MCP servers are unaffected",
		}.pick(en),
	}
}

// gitRemoteRow 建任务副本之前那一次 fetch。
//
// 为什么它必须有一行：这是**设置页一个开关**造出来的出网——「创建前先 fetch」打开之后，
// 每建一个任务副本就往远端问一次。而 worktree 这一页其余内容讲的都是本机动作（建目录、删目录），
// 读者不会自然想到"隔离"这件事会连网。
//
// 与 git.push 的关系：push 也出网，但它是模型在任务里主动调 git.push 触发的，
// 不属于"配置里就存在、随时可能被动用"这一类；这一行只交代 fetch。
func (a *Agent) gitRemoteRow(rep safety.EgressReport, en bool) ConnectionRow {
	r := ConnectionRow{
		ID: "git.remote", Title: bi{
			"远端仓库同步（建副本前的 fetch）",
			"Remote repo sync (fetch before worktree)",
		}.pick(en), Kind: "out", KindText: bi{"出网", "Egress"}.pick(en),
		Target: bi{
			"工作区的 origin 远端（地址读自本机 git 配置；台账只记主机名，不记 URL 全量）",
			"The workspace's origin remote (address read from the local git config; the ledger records only the host name, not the full URL)",
		}.pick(en),
		Trigger: bi{
			"设置 → Worktrees 打开「创建前先 fetch」后，每个任务建 worktree 之前自动跑一次 git fetch",
			"With Settings → Worktrees → \"Fetch before create\" on, a git fetch runs automatically before each task worktree is created",
		}.pick(en),
		Leaves: bi{
			"不外发你的数据；这一下只向远端取分支与提交，取回的内容留在本机",
			"None of your data leaves; this only fetches branches and commits from the remote, and what comes back stays local",
		}.pick(en),
		Trace: bi{
			"门控留痕 action=egress、kind=git.remote（只记主机；git 不报传输字节，所以次数有、字节为 0）",
			"Gate record action=egress, kind=git.remote (host only; git does not report transferred bytes, so counts exist and bytes stay 0)",
		}.pick(en),
		Stats: gitFetchStatsText(rep, en),
		Off: bi{
			"设置 → Worktrees 关掉「创建前先 fetch」；连总开关一起关掉则一个任务副本都不会建",
			"Settings → Worktrees: turn off \"Fetch before create\"; turn off the master switch too and no task worktree is created at all",
		}.pick(en),
	}
	if a.Cfg == nil || !a.Cfg.Worktrees.Enabled || !a.Cfg.Worktrees.FetchBeforeCreate {
		r.Status = bi{
			"关着：worktree 总开关与「创建前先 fetch」没同时打开，这条线不存在",
			"Off: the Worktrees master switch and \"Fetch before create\" are not both on — this line does not exist",
		}.pick(en)
		r.Target = "—"
		r.Leaves = bi{
			"不外发：要么不建副本，要么建的时候不同步远端",
			"Nothing leaves: either no worktree is created, or creation skips the remote",
		}.pick(en)
		return r
	}
	r.Status = bi{
		"开着：每建一个任务副本就 fetch 一次",
		"On: one fetch every time a task worktree is created",
	}.pick(en)
	return r
}

// gitFetchStatsText fetch 的读数不说字节：git 不报传输量，写"累计 0 字节"会被读成
// "什么都没发出去"，而事实是"发了多少这里数不出来"——把不知道说成答案，比不说更坏。
func gitFetchStatsText(rep safety.EgressReport, en bool) string {
	st, ok := egressStat(rep, "git.remote")
	if !ok {
		return bi{
			"本次运行还没有这一类的出网记录",
			"No egress records of this kind in this run yet",
		}.pick(en)
	}
	hosts := strings.Join(st.Hosts, bi{"、", ", "}.pick(en))
	if len(st.Hosts) == 0 {
		hosts = bi{"主机名未知", "host names unknown"}.pick(en)
	}
	return fmt.Sprintf(bi{
		"已出网 %d 次，涉及 %d 个主机（%s）；git 不报传输字节，这里只记次数",
		"%d egress calls, %d host(s) (%s); git does not report transferred bytes, so this counts calls only",
	}.pick(en), st.Count, len(st.Hosts), hosts)
}

// marketRow 内置模板目录：市场随程序一起装好的那部分，装它本身不出网。
//
// 为什么值得单独占一行：别的桌面 Agent 把"市场"当成入口，用户默认那里在联网。
// 这一行是这张表里唯一一条"你以为是连接、其实不是"——把这件事写在台账上，
// 比在功能介绍里说一遍"我们本地优先"更可核查。
func marketRow(en bool) ConnectionRow {
	return ConnectionRow{
		ID: "market", Title: bi{
			"技能 / MCP 模板目录（内置）",
			"Skill / MCP template catalog (built-in)",
		}.pick(en), Kind: "local", KindText: bi{"本机", "Local"}.pick(en),
		Target: bi{
			"内置在程序里，不是一个网络地址",
			"Built into the program — not a network address",
		}.pick(en),
		Status: bi{"离线可用", "Works offline"}.pick(en),
		Trigger: bi{
			"你在市场点「安装」时",
			"When you click \"Install\" in the market",
		}.pick(en),
		Leaves: bi{
			"不外发：安装只往配置与技能库里写条目",
			"Nothing leaves: installing only writes entries into config and the skill library",
		}.pick(en),
		Trace: bi{
			"写配置与技能库有留痕（安装记录在配置里）；这一份目录本身一次请求都不发",
			"Config and skill-library writes are recorded (install entries live in config); this catalog itself never sends a single request",
		}.pick(en),
		Off: bi{
			"不需要关：它不发请求。装完的 MCP 若不想再用，去设置 → MCP 停用",
			"Nothing to turn off: it sends no requests. To stop using an installed MCP, disable it under Settings → MCP",
		}.pick(en),
		Alert: bi{
			"市场页还会去 MCP 官方注册表拉远端目录——那条出网单独记在「市场远端目录」那一行，" +
				"不要拿这一行的「离线可用」去读整张市场页",
			"The market page also pulls a remote catalog from the official MCP registry — that egress has its " +
				"own row (\"Market remote catalog\"). Do not read this row's \"works offline\" as a description of the whole market page",
		}.pick(en),
	}
}

// mcpRemoteRow 远端 MCP：用户从市场装的那几个 streamable-http 服务器。
//
// 为什么它和下面每条 MCP 服务器各占一行、而不是合并：这一行回答的是"这一类动作会不会出网"，
// 下面那些行回答的是"这台机器上装了哪几个、各自通到哪"。两个问题，两行；合并了任一个都答不全。
func mcpRemoteRow(rep safety.EgressReport, en bool) ConnectionRow {
	return ConnectionRow{
		ID: "mcp.remote", Title: bi{"远端 MCP 服务器", "Remote MCP servers"}.pick(en), Kind: "out",
		KindText: bi{"出网", "Egress"}.pick(en),
		Target: bi{
			"你在市场装的远端（streamable-http）MCP 服务器，各自的主机见下面「MCP 服务器」那几行",
			"The remote (streamable-http) MCP servers you installed from the market; see the \"MCP server\" rows below for each host",
		}.pick(en),
		Status: bi{
			"待命：只有模型调用远端工具、或启动时握手才发请求",
			"Standing by: requests only when the model calls a remote tool, or during the startup handshake",
		}.pick(en),
		Trigger: bi{
			"任务执行中模型调用某个 mcp.<服务器>.<工具> 时，以及启动/重连时的握手",
			"When the model calls some mcp.<server>.<tool> during a task, plus the startup/reconnect handshake",
		}.pick(en),
		Leaves: bi{
			"工具参数与结果走这条连接；请求头里的凭据（API Key 之类）也随请求发往对方主机",
			"Tool arguments and results travel this connection; credentials in the request headers (API keys and the like) also go to the remote host with each request",
		}.pick(en),
		Trace: bi{
			"门控留痕 action=egress、kind=mcp.remote（只记主机与字节，不记正文与凭据）",
			"Gate record action=egress, kind=mcp.remote (hosts and bytes only, never body text or credentials)",
		}.pick(en),
		Stats: egressStatsText(rep, "mcp.remote", en),
		Off: bi{
			"设置 → MCP：停用或卸载那台远端服务器；不装任何远端服务器时这条线不存在",
			"Settings → MCP: disable or uninstall that remote server; with no remote servers installed, this line does not exist",
		}.pick(en),
	}
}
