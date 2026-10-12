package agent

import (
	"context"
	"fmt"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"gleam/internal/harness/credentials"
	"gleam/internal/llm"
	"gleam/pkg/types"
)

func rowByID(led ConnectionLedger, id string) (ConnectionRow, bool) {
	for _, r := range led.Rows {
		if r.ID == id {
			return r, true
		}
	}
	return ConnectionRow{}, false
}

func TestConnections_EveryEgressKindHasARow(t *testing.T) {
	f := newFixture(t, nil)
	led := f.a.ConnectionView("127.0.0.1:8787", false)

	listed := map[string]bool{}
	for _, k := range EgressKinds() {
		listed[k] = true
	}
	drawn := map[string]bool{}
	for _, r := range led.Rows {
		if r.Kind != "out" {
			continue
		}
		drawn[r.ID] = true
	}
	for _, kind := range EgressKinds() {
		r, ok := rowByID(led, kind)
		if !ok {
			t.Errorf("出网清单里的 %q 在台账上没有对应的行：这条线在界面上等于不存在", kind)
			continue
		}
		if r.Kind != "out" {
			t.Errorf("%q 这一行的 kind = %q，出网落点必须是 out", kind, r.Kind)
		}
	}
	for id := range drawn {
		if !listed[id] {
			t.Errorf("台账画了出网行 %q，但它不在出网清单里：这一行不会有留痕读数，空白格子的读法是\"不归我管\"", id)
		}
	}
}

func TestConnections_NoRowHasAnEmptyCell(t *testing.T) {
	f := newFixture(t, nil)
	for _, bind := range []string{"", "127.0.0.1:8787", "0.0.0.0:8787"} {
		for _, r := range f.a.ConnectionView(bind, false).Rows {
			for _, field := range []struct{ name, val string }{
				{"title", r.Title}, {"kindText", r.KindText}, {"target", r.Target},
				{"status", r.Status}, {"trigger", r.Trigger}, {"leaves", r.Leaves},
				{"trace", r.Trace}, {"off", r.Off},
			} {
				if strings.TrimSpace(field.val) == "" {
					t.Errorf("bind=%q 行 %q 的 %s 是空的", bind, r.ID, field.name)
				}
			}
			if r.Kind == "out" && strings.TrimSpace(r.Stats) == "" {
				t.Errorf("bind=%q 出网行 %q 没有读数文案（没记录也要说成没记录）", bind, r.ID)
			}
			if r.Untraced && !strings.Contains(r.Trace, "没有留痕") {
				t.Errorf("行 %q 标了未留痕，Trace 却没说自己没留痕：%q", r.ID, r.Trace)
			}
		}
	}
}

func TestConnections_InboundRowFollowsRealBindAddr(t *testing.T) {
	f := newFixture(t, nil)

	if _, ok := rowByID(f.a.ConnectionView("", false), "inbound"); ok {
		t.Error("拿不到监听地址时不该画本机服务这一行")
	}

	loop, ok := rowByID(f.a.ConnectionView("127.0.0.1:8787", false), "inbound")
	if !ok {
		t.Fatal("绑了回环就该有这一行")
	}
	if loop.Kind != "in" || loop.Target != "127.0.0.1:8787" {
		t.Errorf("回环行 = %+v", loop)
	}
	if !strings.Contains(loop.Alert, "回环") {
		t.Errorf("回环应说明\"只有本机能连、口令文件同用户可读\"：%q", loop.Alert)
	}

	open, ok := rowByID(f.a.ConnectionView("0.0.0.0:8787", false), "inbound")
	if !ok {
		t.Fatal("绑了 0.0.0.0 也该有这一行")
	}
	if !strings.Contains(open.Status, "非回环") || !strings.Contains(open.Alert, "局域网") {
		t.Errorf("非回环必须改口报警：status=%q alert=%q", open.Status, open.Alert)
	}
}

func TestConnections_LlmRowTargetMatchesKeyScope(t *testing.T) {
	f := newFixture(t, nil)
	f.a.Cfg.LLM.Provider = "glm"
	f.a.Cfg.LLM.BaseURL = "https://api.example.com/v1"

	r, ok := rowByID(f.a.ConnectionView("", false), "llm")
	if !ok {
		t.Fatal("缺模型行")
	}
	want := llm.KeyScope(f.a.Cfg.LLM.BaseURL)
	if want == "" {
		t.Fatal("前提不成立：KeyScope 解析不出主机")
	}
	if r.Target != want {
		t.Errorf("模型行目标 = %q，应等于密钥绑定的主机 %q", r.Target, want)
	}
	if r.Status != "开着" {
		t.Errorf("配了真实接入应说\"开着\"，得到 %q", r.Status)
	}
	if !strings.Contains(r.Leaves, "提示词") {
		t.Errorf("这一行的重点是把提示词全文发出去：%q", r.Leaves)
	}

	f.a.Gate.RecordEgress("llm", "old.vendor.com", 2048)
	r2, _ := rowByID(f.a.ConnectionView("", false), "llm")
	if !strings.Contains(r2.Alert, "old.vendor.com") {
		t.Errorf("留痕与当前接入不一致时要点名：%q", r2.Alert)
	}
	if !strings.Contains(r2.Stats, "1 次") || !strings.Contains(r2.Stats, "2.0 KB") {
		t.Errorf("读数没接上门控留痕：%q", r2.Stats)
	}
}

func TestConnections_MockModelSaysTheLineCannotSend(t *testing.T) {
	f := newFixture(t, nil)
	r, ok := rowByID(f.a.ConnectionView("", false), "llm")
	if !ok {
		t.Fatal("缺模型行")
	}
	if !strings.Contains(r.Status, "发不出东西") || !strings.Contains(r.Leaves, "不外发") {
		t.Errorf("mock 下的说法 = status %q / leaves %q", r.Status, r.Leaves)
	}
}

func TestConnections_WebFetchStatusComesFromGate(t *testing.T) {
	f := newFixture(t, nil)
	f.a.Reg.MustRegister(&funcTool{name: "web.fetch", perm: types.PermissionReadOnly,
		fn: func(context.Context, map[string]any) (any, error) { return "ok", nil }})

	r, _ := rowByID(f.a.ConnectionView("", false), "web.fetch")
	if !strings.Contains(r.Status, "自动放行") {
		t.Errorf("只读权限应说自动放行：%q", r.Status)
	}
	if !strings.Contains(r.Target, "模型") {
		t.Errorf("这一行的关键是网址由模型自己挑，不是用户配的：%q", r.Target)
	}

	f.a.Gate.SetToolPermission("web.fetch", types.PermissionUserApproved)
	r2, _ := rowByID(f.a.ConnectionView("", false), "web.fetch")
	if !strings.Contains(r2.Status, "每次问你") {
		t.Errorf("权限改紧后应改口：%q", r2.Status)
	}

	f.a.Cfg.Safety.AllowPrivateWeb = true
	r3, _ := rowByID(f.a.ConnectionView("", false), "web.fetch")
	if !strings.Contains(r3.Alert, "内网") {
		t.Errorf("放开内网抓取必须警示：%q", r3.Alert)
	}
}

func TestConnections_CloudAndFeedbackAbsentWhenUnconfigured(t *testing.T) {
	f := newFixture(t, nil)

	for _, id := range []string{"cloud", "feedback"} {
		r, ok := rowByID(f.a.ConnectionView("", false), id)
		if !ok {
			t.Fatalf("缺 %s 行", id)
		}
		if !strings.Contains(r.Status, "没装配") {
			t.Errorf("%s 行状态 = %q", id, r.Status)
		}
	}

	cred, err := credentials.Open(f.a.Cfg.DataDir)
	if err != nil {
		t.Fatal(err)
	}
	f.a.Creds = cred

	cloud, _ := rowByID(f.a.ConnectionView("", false), "cloud")
	if !strings.Contains(cloud.Status, "未配置") {
		t.Errorf("没配项目地址应说未配置：%q", cloud.Status)
	}
	fb, _ := rowByID(f.a.ConnectionView("", false), "feedback")
	if !strings.Contains(fb.Status, "只落本机") {
		t.Errorf("反馈没配远端应说只落本机：%q", fb.Status)
	}

	if err := cred.SetCloudConfig(credentials.CloudConfig{SupabaseURL: "https://proj.supabase.co"}); err != nil {
		t.Fatal(err)
	}
	half, _ := rowByID(f.a.ConnectionView("", false), "feedback")
	if !strings.Contains(half.Status, "未配置") {
		t.Errorf("缺 anon key 不该算配好：%q", half.Status)
	}

	if err := cred.SetCloudConfig(credentials.CloudConfig{SupabaseAnonKey: "anon"}); err != nil {
		t.Fatal(err)
	}
	full, _ := rowByID(f.a.ConnectionView("", false), "cloud")
	if full.Status != "已配置" || full.Target != llm.KeyScope("https://proj.supabase.co") {
		t.Errorf("配好后 = %+v", full)
	}
}

func TestConnections_EgressScopeTextFollowsScanWindow(t *testing.T) {
	f := newFixture(t, nil)
	if s := f.a.ConnectionView("", false).EgressScope; !strings.Contains(s, "还没有任何出网留痕") {
		t.Errorf("无留痕时的范围文案 = %q", s)
	} else if strings.Contains(s, "--audit") {
		t.Errorf("审计没落盘却让人去查落盘审计：%q", s)
	}

	f.a.Gate.RecordEgress("llm", "api.example.com", 10)
	if s := f.a.ConnectionView("", false).EgressScope; !strings.Contains(s, "本次运行内存里") {
		t.Errorf("有留痕但未写满时应说明只数了本次运行：%q", s)
	}

	f.a.Gate.SetAuditPath(filepath.Join(f.a.Cfg.DataDir, "audit.jsonl"))
	if s := f.a.ConnectionView("", false).EgressScope; !strings.Contains(s, "--audit --egress") {
		t.Errorf("落了盘就该给出回看入口：%q", s)
	}
	f.a.Gate.SetAuditPath("")
	if s := f.a.ConnectionView("", false).EgressScope; strings.Contains(s, "--audit") {
		t.Errorf("落盘撤掉后指路必须跟着撤：%q", s)
	}
}

func TestConnections_EgressScopeAdmitsRingOverflow(t *testing.T) {
	f := newFixture(t, nil)
	for i := 0; i < 240; i++ {
		f.a.Gate.RecordEgress("llm", "api.example.com", 1)
	}
	led := f.a.ConnectionView("", false)
	if !strings.Contains(led.EgressScope, "环已写满") {
		t.Errorf("留痕溢出仍说\"本次运行\"：%q", led.EgressScope)
	}
	r, _ := rowByID(led, "llm")
	if !strings.Contains(r.Stats, "已出网") {
		t.Errorf("溢出时模型行该有读数：%q", r.Stats)
	}
}

func TestConnections_MarketRowIsNotAConnection(t *testing.T) {
	f := newFixture(t, nil)
	r, ok := rowByID(f.a.ConnectionView("", false), "market")
	if !ok {
		t.Fatal("缺市场行")
	}
	if r.Kind != "local" {
		t.Errorf("市场不是网络边界，kind = %q", r.Kind)
	}
	if !strings.Contains(r.Leaves, "不外发") || !strings.Contains(r.Status, "离线") {
		t.Errorf("市场行必须说清它不发包：status=%q leaves=%q", r.Status, r.Leaves)
	}
	if r.Stats != "" {
		t.Errorf("不发东西的行不该有出网读数：%q", r.Stats)
	}
}

func hasCJK(s string) bool {
	for _, r := range s {
		switch {
		case r >= 0x3000 && r <= 0x303f,
			r >= 0x4e00 && r <= 0x9fff,
			r >= 0xf900 && r <= 0xfaff,
			r >= 0xff00 && r <= 0xffef:
			return true
		}
	}
	return false
}

func assertNoCJK(t *testing.T, path string, v any) {
	t.Helper()
	var walk func(path string, rv reflect.Value)
	walk = func(path string, rv reflect.Value) {
		switch rv.Kind() {
		case reflect.String:
			if hasCJK(rv.String()) {
				t.Errorf("%s：英文输出里还有 CJK 字符：%q", path, rv.String())
			}
		case reflect.Struct:
			for i := 0; i < rv.NumField(); i++ {
				walk(path+"."+rv.Type().Field(i).Name, rv.Field(i))
			}
		case reflect.Slice, reflect.Array:
			for i := 0; i < rv.Len(); i++ {
				walk(fmt.Sprintf("%s[%d]", path, i), rv.Index(i))
			}
		case reflect.Pointer:
			if !rv.IsNil() {
				walk(path, rv.Elem())
			}
		}
	}
	walk(path, reflect.ValueOf(v))
}

func TestConnections_EnglishLedgerHasNoCJK(t *testing.T) {
	f := newFixture(t, nil)
	for _, bind := range []string{"", "127.0.0.1:8787"} {
		assertNoCJK(t, "connections("+bind+")", f.a.ConnectionView(bind, true))
	}
	if !hasCJK(f.a.ConnectionView("", false).Scope) {
		t.Error("中文版 Scope 不含中文：bi.pick(false) 坏了，两份说法被改成了同一份")
	}
}
