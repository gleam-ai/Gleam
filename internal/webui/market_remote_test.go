package webui

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"gleam/internal/market"
)

const registryFixture = `{
  "servers": [
    {"server": {
      "name": "io.example/npm-server", "title": "NPM Server", "description": "一个能装的 npm 服务器",
      "version": "1.2.3",
      "repository": {"url": "https://example.invalid/npm-server"},
      "packages": [{
        "registryType": "npm", "registryBaseUrl": "https://registry.npmjs.org",
        "identifier": "@example/npm-server", "version": "1.2.3", "runtimeHint": "npx",
        "transport": {"type": "stdio"},
        "runtimeArguments": [{"value": "-y", "type": "positional"}],
        "environmentVariables": [
          {"name": "API_TOKEN", "description": "服务令牌", "isRequired": true},
          {"name": "OPTIONAL_ONE", "description": "可选", "isRequired": false}
        ]
      }]
    }},
    {"server": {
      "name": "io.example/plain-npm", "title": "Plain NPM", "description": "不需要环境变量的 npm 服务器",
      "version": "2.0.0",
      "packages": [{
        "registryType": "npm", "identifier": "@example/plain", "version": "2.0.0", "runtimeHint": "npx",
        "transport": {"type": "stdio"},
        "runtimeArguments": [{"value": "-y", "type": "positional"}]
      }]
    }},
    {"server": {
      "name": "ac.example/remote-only", "title": "Remote Only", "description": "只有远端传输",
      "version": "0.1.0",
      "remotes": [{"type": "streamable-http", "url": "http://127.0.0.1:9/mcp"}]
    }},
    {"server": {
      "name": "legacy.example/sse-only", "title": "Legacy SSE", "description": "只有旧版 sse",
      "version": "0.1.0",
      "remotes": [{"type": "sse", "url": "https://old.invalid/sse"}]
    }},
    {"server": {
      "name": "io.example/cargo-server", "title": "Cargo Server", "description": "只有 cargo 包",
      "version": "0.0.1",
      "packages": [{"registryType": "cargo", "identifier": "cargo-server", "version": "0.0.1", "runtimeHint": ""}]
    }}
  ]
}`

func TestParseRegistryClassifiesEveryShape(t *testing.T) {
	presets, err := market.ParseRegistry([]byte(registryFixture), "official-mcp")
	if err != nil {
		t.Fatal(err)
	}
	if len(presets) != 5 {
		t.Fatalf("应解析出 5 条，得到 %d", len(presets))
	}
	byID := map[string]market.RemotePreset{}
	for _, p := range presets {
		byID[p.ID] = p
	}

	plain := byID["io.example/plain-npm"]
	if !plain.Installable {
		t.Fatalf("不需要环境变量的 npm stdio 条目应可安装：%+v", plain)
	}
	if plain.Command != "npx" {
		t.Errorf("运行时应是 npx，得到 %q", plain.Command)
	}
	if len(plain.BaseArgs) == 0 || plain.BaseArgs[0] != "-y" {
		t.Errorf("runtimeArguments 应排在包引用之前：%v", plain.BaseArgs)
	}
	if !strings.Contains(strings.Join(plain.BaseArgs, " "), "@example/plain@2.0.0") {
		t.Errorf("参数里应带包引用与版本：%v", plain.BaseArgs)
	}
	if plain.PkgRef != "@example/plain@2.0.0" {
		t.Errorf("PkgRef 应记包引用：%q", plain.PkgRef)
	}

	needEnv := byID["io.example/npm-server"]
	if !needEnv.Installable {
		t.Fatalf("需要环境变量的条目应可安装（弹窗里填）：%+v", needEnv)
	}
	if len(needEnv.Params) != 2 {
		t.Fatalf("必填与可选环境变量都该收进来，得到 %+v", needEnv.Params)
	}
	byKey := map[string]market.Param{}
	for _, pm := range needEnv.Params {
		byKey[pm.Key] = pm
	}
	if pm := byKey["API_TOKEN"]; pm.Kind != "env" || !pm.Required {
		t.Errorf("API_TOKEN 应是必填的环境变量：%+v", pm)
	}
	if pm := byKey["OPTIONAL_ONE"]; pm.Kind != "env" || pm.Required {
		t.Errorf("可选环境变量应标成非必填：%+v", pm)
	}
	if needEnv.Homepage != "https://example.invalid/npm-server" {
		t.Errorf("homepage 应取自 repository.url：%q", needEnv.Homepage)
	}

	remote := byID["ac.example/remote-only"]
	if !remote.Installable || remote.Kind != "http" || remote.URL == "" {
		t.Errorf("streamable-http 型远端应可安装并标成 http：%+v", remote)
	}
	legacy := byID["legacy.example/sse-only"]
	if legacy.Installable || !strings.Contains(legacy.Unsupported, "sse") {
		t.Errorf("只有 sse 的条目应不可安装并点名 sse：%+v", legacy)
	}

	cargo := byID["io.example/cargo-server"]
	if cargo.Installable || !strings.Contains(cargo.Unsupported, "cargo") {
		t.Errorf("只有 cargo 包的条目应不可安装并点名分发方式：%+v", cargo)
	}
}

func TestFilterRemoteMatchesBuiltinKeywordRules(t *testing.T) {
	presets, err := market.ParseRegistry([]byte(registryFixture), "official-mcp")
	if err != nil {
		t.Fatal(err)
	}
	if got := market.FilterRemote(presets, ""); len(got) != 5 {
		t.Errorf("空关键词应返回全部，得到 %d", len(got))
	}
	if got := market.FilterRemote(presets, "远端传输"); len(got) != 1 || got[0].ID != "ac.example/remote-only" {
		t.Errorf("应命中描述里的词，得到 %+v", got)
	}
	if got := market.FilterRemote(presets, "不存在的词"); len(got) != 0 {
		t.Errorf("无命中应返回空，得到 %d", len(got))
	}
}

func TestMarketInstallArgsOnlyRewritesNPM(t *testing.T) {
	base := []string{"-y", "@example/npm-server@1.2.3"}
	cn := market.Source{ID: "npm-cn", Kind: "npm", BaseURL: "https://registry.npmmirror.com"}
	official := market.Source{ID: "npm", Kind: "npm", BaseURL: "https://registry.npmjs.org"}

	got := marketInstallArgs(cn, true, base)
	if len(got) != 3 || got[0] != "--registry=https://registry.npmmirror.com" {
		t.Errorf("国内源 + npm 包应前置 --registry：%v", got)
	}
	if out := marketInstallArgs(official, true, base); len(out) != len(base) {
		t.Errorf("官方源不该加参数：%v", out)
	}
	if out := marketInstallArgs(cn, false, base); len(out) != len(base) {
		t.Errorf("非 npm 包不该加 --registry：%v", out)
	}
}

func TestMarketRemoteFetchCacheAndEgress(t *testing.T) {
	f := newFixture(t, nil)

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/v0/servers" {
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(registryFixture))
			return
		}
		_, _ = w.Write([]byte(`{"name":"left-pad"}`))
	}))
	defer ts.Close()

	oldCatalog, oldNPM, oldCN := market.MarketCatalogURL, market.MarketNPMURL, market.MarketNPMCNURL
	market.MarketCatalogURL = ts.URL + "/v0/servers"
	market.MarketNPMURL = ts.URL + "/npm"
	market.MarketNPMCNURL = ts.URL + "/npm-cn"
	defer func() {
		market.MarketCatalogURL, market.MarketNPMURL, market.MarketNPMCNURL = oldCatalog, oldNPM, oldCN
	}()

	oldState := marketCacheState
	marketCacheState = &marketState{}
	defer func() { marketCacheState = oldState }()

	out := f.call("GET", "/api/market/mcp?q=", nil)
	if out["remote_count"] != float64(5) {
		t.Fatalf("远端应拉到 4 条：%v（note=%v）", out["remote_count"], out["source_note"])
	}
	presets, _ := out["presets"].([]any)
	remoteSeen := 0
	for _, it := range presets {
		if m, ok := it.(map[string]any); ok && m["remote"] == true {
			remoteSeen++
		}
	}
	if remoteSeen != 5 {
		t.Errorf("合并结果里应有 4 条带 remote 标记的条目，得到 %d", remoteSeen)
	}

	rep := f.agent.Gate.EgressStats()
	found := false
	for _, st := range rep.Stats {
		if st.Kind == "market.remote" && st.Count > 0 {
			found = true
		}
	}
	if !found {
		t.Errorf("market.remote 没有留痕：%+v", rep.Stats)
	}

	market.MarketCatalogURL = "http://127.0.0.1:1/v0/servers"
	marketCacheState = &marketState{}
	fallback := f.call("GET", "/api/market/mcp?refresh=1", nil)
	note, _ := fallback["source_note"].(string)
	if !strings.Contains(note, "缓存") {
		t.Errorf("断网时应说明用的是缓存，得到 note=%q", note)
	}
	if fallback["remote_count"] != float64(5) {
		t.Errorf("断网也应给出缓存里的 4 条，得到 %v", fallback["remote_count"])
	}
}

func TestMarketSourcesEndpoint(t *testing.T) {
	f := newFixture(t, nil)
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"name":"left-pad"}`))
	}))
	defer ts.Close()

	oldNPM, oldCN := market.MarketNPMURL, market.MarketNPMCNURL
	market.MarketNPMURL, market.MarketNPMCNURL = ts.URL+"/npm", ts.URL+"/npm-cn"
	defer func() { market.MarketNPMURL, market.MarketNPMCNURL = oldNPM, oldCN }()
	oldState := marketCacheState
	marketCacheState = &marketState{}
	defer func() { marketCacheState = oldState }()

	out := f.call("GET", "/api/market/sources", nil)
	srcs, _ := out["sources"].([]any)
	if len(srcs) < 4 {
		t.Fatalf("源表至少应有 官方注册表 / npm / npm 镜像 / 内置，得到 %d", len(srcs))
	}
	if out["catalog_source"] != "official-mcp" {
		t.Errorf("目录源应标成 official-mcp：%v", out["catalog_source"])
	}
	why, _ := out["npm_source_why"].(string)
	if strings.TrimSpace(why) == "" {
		t.Error("必须说清为什么选了这个 npm 源")
	}
	if strings.Contains(why, "IP") || strings.Contains(why, "归属") {
		t.Errorf("选源理由不该提 IP 归属地：%q", why)
	}
	if !strings.Contains(out["note"].(string), "延迟") {
		t.Errorf("说明里应点出选择方式是实测延迟：%v", out["note"])
	}
}

func TestParseRegistryRejectsGarbage(t *testing.T) {
	if _, err := market.ParseRegistry([]byte("not json"), "official-mcp"); err == nil {
		t.Error("非 JSON 应当报错")
	}
	var resp map[string]any
	if err := json.Unmarshal([]byte(registryFixture), &resp); err != nil {
		t.Fatalf("fixture 本身应是合法 JSON：%v", err)
	}
}

func TestRemoteInstallRefusesTheUninstallable(t *testing.T) {
	f := newFixture(t, nil)
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/v0/servers" {
			_, _ = w.Write([]byte(registryFixture))
			return
		}
		_, _ = w.Write([]byte(`{"name":"left-pad"}`))
	}))
	defer ts.Close()

	oldCatalog, oldNPM, oldCN := market.MarketCatalogURL, market.MarketNPMURL, market.MarketNPMCNURL
	market.MarketCatalogURL, market.MarketNPMURL, market.MarketNPMCNURL = ts.URL+"/v0/servers", ts.URL+"/npm", ts.URL+"/npm-cn"
	defer func() {
		market.MarketCatalogURL, market.MarketNPMURL, market.MarketNPMCNURL = oldCatalog, oldNPM, oldCN
	}()
	oldState := marketCacheState
	marketCacheState = &marketState{}
	defer func() { marketCacheState = oldState }()

	inst := f.call("POST", "/api/market/mcp/install", map[string]any{"id": "io.example/plain-npm"})
	if inst["installed"] != true {
		t.Fatalf("可安装的远端条目应装上：%v", inst)
	}
	if _, ok := inst["name"]; !ok {
		t.Errorf("安装结果应带服务器名：%v", inst)
	}

	remoteInst := f.call("POST", "/api/market/mcp/install", map[string]any{"id": "ac.example/remote-only"})
	if remoteInst["installed"] != true {
		t.Fatalf("远端条目应装上（配置落盘）：%v", remoteInst)
	}
	if w, _ := remoteInst["warning"].(string); w == "" {
		t.Errorf("连不上时应给出 warning，而不是假装连上了：%v", remoteInst)
	}
	if code, body := f.status("POST", "/api/market/mcp/install", `{"id":"legacy.example/sse-only"}`); code != 400 {
		t.Errorf("只有 sse 的条目应被拒（400），得到 %d %s", code, body)
	} else if !strings.Contains(body, "sse") {
		t.Errorf("拒绝理由应点名 sse：%s", body)
	}

	if code, body := f.status("POST", "/api/market/mcp/install", `{"id":"io.example/npm-server"}`); code != 400 {
		t.Errorf("没填必填环境变量应被拒（400），得到 %d %s", code, body)
	} else if !strings.Contains(body, "API_TOKEN") {
		t.Errorf("拒绝理由应点名缺哪个变量：%s", body)
	}
	f.call("POST", "/api/market/mcp/install", map[string]any{
		"id": "io.example/npm-server", "params": map[string]string{"API_TOKEN": "secret-value"},
	})
	foundEnv := ""
	for _, sv := range f.agent.Cfg.MCP {
		if sv.Name == "npm-server" {
			foundEnv = sv.Env["API_TOKEN"]
		}
	}
	if foundEnv != "secret-value" {
		t.Errorf("填了的环境变量应落进 MCP 配置，得到 %q", foundEnv)
	}
	mcpList := f.call("GET", "/api/mcp", nil)["mcp"].([]any)
	for _, it := range mcpList {
		m, _ := it.(map[string]any)
		if _, leaked := m["env"]; leaked {
			t.Errorf("/api/mcp 不该回吐环境变量（里面是密钥）：%v", m)
		}
	}

	if code, _ := f.status("POST", "/api/market/mcp/install", `{"id":"no.such/entry"}`); code != 400 {
		t.Errorf("未知 id 应 400，得到 %d", code)
	}
}

func TestMarketSearchForwardsQueryToSource(t *testing.T) {
	f := newFixture(t, nil)

	var mu sync.Mutex
	var seenSearch string
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/v0/servers" {
			mu.Lock()
			seenSearch = r.URL.Query().Get("search")
			mu.Unlock()
			_, _ = w.Write([]byte(registryFixture))
			return
		}
		_, _ = w.Write([]byte(`{"name":"left-pad"}`))
	}))
	defer ts.Close()

	oldCatalog, oldNPM, oldCN := market.MarketCatalogURL, market.MarketNPMURL, market.MarketNPMCNURL
	market.MarketCatalogURL, market.MarketNPMURL, market.MarketNPMCNURL = ts.URL+"/v0/servers", ts.URL+"/npm", ts.URL+"/npm-cn"
	defer func() {
		market.MarketCatalogURL, market.MarketNPMURL, market.MarketNPMCNURL = oldCatalog, oldNPM, oldCN
	}()
	oldState := marketCacheState
	marketCacheState = &marketState{}
	defer func() { marketCacheState = oldState }()

	f.call("GET", "/api/market/mcp?q=filesystem", nil)
	mu.Lock()
	got := seenSearch
	mu.Unlock()
	if got != "filesystem" {
		t.Errorf("查询词应原样发给目录源，得到 search=%q", got)
	}

	mu.Lock()
	seenSearch = ""
	mu.Unlock()
	f.call("GET", "/api/market/mcp?q=", nil)
	mu.Lock()
	empty := seenSearch
	mu.Unlock()
	if empty != "" {
		t.Errorf("空查询不该带 search 参数，得到 %q", empty)
	}
}

func TestParseRegistryKeepsNewestVersionPerServer(t *testing.T) {
	body := `{"servers":[
      {"server":{"name":"io.example/multi","title":"Multi","description":"多版本",
        "version":"1.9.0","packages":[{"registryType":"npm","identifier":"multi","version":"1.9.0","runtimeHint":"npx","transport":{"type":"stdio"}}]}},
      {"server":{"name":"io.example/multi","title":"Multi","description":"多版本",
        "version":"1.10.0","packages":[{"registryType":"npm","identifier":"multi","version":"1.10.0","runtimeHint":"npx","transport":{"type":"stdio"}}]}},
      {"server":{"name":"io.example/multi","title":"Multi","description":"多版本",
        "version":"1.2.0","packages":[{"registryType":"npm","identifier":"multi","version":"1.2.0","runtimeHint":"npx","transport":{"type":"stdio"}}]}}
    ]}`
	presets, err := market.ParseRegistry([]byte(body), "official-mcp")
	if err != nil {
		t.Fatal(err)
	}
	if len(presets) != 1 {
		t.Fatalf("同名多版本应去重成 1 条，得到 %d：%+v", len(presets), presets)
	}
	if presets[0].Version != "1.10.0" {
		t.Errorf("应保留最高版本 1.10.0，得到 %q", presets[0].Version)
	}
}

func TestRemoteInstallFindsEntryFromSearchNotJustBrowse(t *testing.T) {
	f := newFixture(t, nil)
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v0/servers" {
			_, _ = w.Write([]byte(`{"name":"left-pad"}`))
			return
		}
		if r.URL.Query().Get("search") == "" {
			_, _ = w.Write([]byte(`{"servers":[]}`))
			return
		}
		_, _ = w.Write([]byte(`{"servers":[{"server":{"name":"io.example/only-by-search","title":"Only","description":"只在搜索里出现",
          "version":"1.0.0","packages":[{"registryType":"npm","identifier":"only-by-search","version":"1.0.0","runtimeHint":"npx","transport":{"type":"stdio"}}]}}]}`))
	}))
	defer ts.Close()

	oldCatalog, oldNPM, oldCN := market.MarketCatalogURL, market.MarketNPMURL, market.MarketNPMCNURL
	market.MarketCatalogURL, market.MarketNPMURL, market.MarketNPMCNURL = ts.URL+"/v0/servers", ts.URL+"/npm", ts.URL+"/npm-cn"
	defer func() {
		market.MarketCatalogURL, market.MarketNPMURL, market.MarketNPMCNURL = oldCatalog, oldNPM, oldCN
	}()
	oldState := marketCacheState
	marketCacheState = &marketState{}
	defer func() { marketCacheState = oldState }()

	inst := f.call("POST", "/api/market/mcp/install", map[string]any{"id": "io.example/only-by-search"})
	if inst["installed"] != true {
		t.Fatalf("应能从搜索结果里找到并装上：%v", inst)
	}
}
