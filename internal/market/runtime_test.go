package market

import (
	"strings"
	"testing"
)

func TestRuntimeForMapsKnownCommands(t *testing.T) {
	cases := map[string]string{
		"npx":                 "npx",
		"npx.cmd":             "npx",
		"NPX":                 "npx",
		"uvx":                 "uvx",
		"/usr/local/bin/uvx":  "uvx",
		`C:\tools\docker.exe`: "docker",
		"cargo":               "cargo",
		"python":              "",
		"my-server":           "",
		"":                    "",
	}
	for cmd, want := range cases {
		if got := RuntimeFor(cmd); got != want {
			t.Errorf("RuntimeFor(%q) = %q，期望 %q", cmd, got, want)
		}
	}
}

func TestRuntimeMissingIgnoresUnknownCommands(t *testing.T) {
	for _, cmd := range []string{"my-server", "/opt/bin/thing", "python3", ""} {
		if st, missing := RuntimeMissing(cmd); missing {
			t.Errorf("%q 不该被判成缺运行时（得到 %+v）", cmd, st)
		}
	}
}

func TestRuntimesHaveActionableWhy(t *testing.T) {
	list := Runtimes()
	if len(list) < 3 {
		t.Fatalf("运行时表太短：%d", len(list))
	}
	for _, r := range list {
		if strings.TrimSpace(r.Why) == "" {
			t.Errorf("%s 缺了之后没有说法（用户会卡在这一步）", r.Name)
		}
		if !strings.Contains(r.Why, "http") && !strings.Contains(r.Why, "设置") {
			t.Errorf("%s 的说法里既没有下载地址也没有站内入口，照着做不了：%q", r.Name, r.Why)
		}
		if r.Found && strings.TrimSpace(r.Path) == "" {
			t.Errorf("%s 说找到了却没给路径", r.Name)
		}
	}
}

func TestRankRemotePutsInstallableFirst(t *testing.T) {
	in := []RemotePreset{
		{ID: "ai.smithery/figma", Name: "Figma", Installable: false},
		{ID: "com.figma.mcp/mcp", Name: "Figma MCP", Installable: true},
		{ID: "x/other", Name: "Other", Installable: true},
	}
	out := RankRemote(in, "figma")
	if !out[0].Installable {
		t.Fatalf("能装的应排在最前：%+v", out)
	}
	if out[0].Name != "Figma MCP" {
		t.Errorf("名字更贴近查询的应更靠前，得到 %q", out[0].Name)
	}
	if out[len(out)-1].Installable {
		t.Errorf("装不了的应排在后面：%+v", out)
	}
	empty := RankRemote(in, "")
	if !empty[0].Installable {
		t.Errorf("空查询也应能装的在前：%+v", empty)
	}
}
