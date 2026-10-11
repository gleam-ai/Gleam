package webui

import (
	"encoding/json"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"gleam/internal/agent"
	"gleam/internal/config"
	"gleam/internal/harness/credentials"
	"gleam/internal/llm"
	"gleam/pkg/types"
)

func newArchiveFixture(t *testing.T) (*Server, string) {
	t.Helper()
	dataDir := t.TempDir()
	cfg := config.Default()
	cfg.DataDir = dataDir
	if err := os.MkdirAll(filepath.Join(dataDir, "tasks"), 0o755); err != nil {
		t.Fatal(err)
	}
	a := agent.New(cfg, nil, nil, nil, nil, nil, nil, nil)
	return NewServer(a), dataDir
}

func writeArchive(t *testing.T, dataDir, id string, g types.GoalResult) {
	t.Helper()
	if err := agent.SaveTaskResult(dataDir, &g); err != nil {
		t.Fatalf("归档失败：%v", err)
	}
}

func TestGoalGet_FallsBackToArchive(t *testing.T) {
	srv, dataDir := newArchiveFixture(t)
	writeArchive(t, dataDir, "task-archived-1", types.GoalResult{
		TaskID: "task-archived-1",
		Goal:   "昨晚跑的那个",
		Status: types.GoalSuccess,
		Score:  95,
	})

	ts := newTokenTestServer(srv)
	defer ts.Close()

	resp, err := http.Get(ts.URL + "/api/goals/task-archived-1")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("盘上有档案时应回 200，实得 %d", resp.StatusCode)
	}
	var got struct {
		TaskID string           `json:"task_id"`
		Goal   string           `json:"goal"`
		Status types.GoalStatus `json:"status"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&got); err != nil {
		t.Fatal(err)
	}
	if got.TaskID != "task-archived-1" || got.Goal != "昨晚跑的那个" || got.Status != types.GoalSuccess {
		t.Errorf("归档回落的内容不对：%+v", got)
	}
}

func TestGoalGet_MemoryWinsOverArchive(t *testing.T) {
	srv, dataDir := newArchiveFixture(t)
	writeArchive(t, dataDir, "dup", types.GoalResult{
		TaskID: "dup", Goal: "档案里的旧版本", Status: types.GoalFailed,
	})
	srv.mu.Lock()
	srv.tasks["dup"] = &taskInfo{ID: "dup", Goal: "内存里的新版本", Status: types.GoalRunning}
	srv.mu.Unlock()

	ts := newTokenTestServer(srv)
	defer ts.Close()

	resp, err := http.Get(ts.URL + "/api/goals/dup")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	var got struct {
		Goal   string           `json:"goal"`
		Status types.GoalStatus `json:"status"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&got); err != nil {
		t.Fatal(err)
	}
	if got.Goal != "内存里的新版本" || got.Status != types.GoalRunning {
		t.Errorf("内存表里的任务应优先于档案，实得 %+v", got)
	}
}

func TestGoalGet_NoArchiveStill404(t *testing.T) {
	srv, _ := newArchiveFixture(t)
	ts := newTokenTestServer(srv)
	defer ts.Close()

	resp, err := http.Get(ts.URL + "/api/goals/nope")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("没有档案时应回 404，实得 %d", resp.StatusCode)
	}
}

func TestGoalSubmit_ArchivesForReplay(t *testing.T) {
	f := newFixture(t, []llm.Scripted{
		{Kind: "plan", Texts: []string{`{"steps":[
			{"id":"s1","description":"写文件","tool":"file.write","args":{"path":"arch.txt","content":"留档"}},
			{"id":"s2","description":"回复","tool":"reply","args":{"text":"留档完成"},"depends_on":["s1"]}
		]}`}},
		{Kind: "reflect", Texts: []string{`{"score":95,"verdict":"done","reason":"ok"}`}},
	})
	out := f.call("POST", "/api/goals", map[string]any{"goal": "创建 arch.txt", "mode": "auto"})
	id, _ := out["task_id"].(string)
	if id == "" {
		t.Fatalf("submit = %v", out)
	}
	if done := f.waitTask(id, 10*time.Second); done["status"] != "success" {
		t.Fatalf("任务没跑成：%v", done)
	}

	g, err := agent.ReadTaskResult(f.dataDir, id)
	if err != nil || g == nil {
		t.Fatalf("界面任务应归档到 tasks/：读到=%v err=%v", g, err)
	}
	if g.Goal != "创建 arch.txt" || g.Status != types.GoalSuccess {
		t.Errorf("归档内容不对：goal=%q status=%q", g.Goal, g.Status)
	}

	f.srv.mu.Lock()
	f.srv.tasks = map[string]*taskInfo{}
	f.srv.mu.Unlock()
	detail := f.call("GET", "/api/goals/"+id, nil)
	if detail["status"] != "success" || detail["goal"] != "创建 arch.txt" {
		t.Errorf("清空内存表后应回落到归档，实得 %v", detail)
	}
}

func TestGoalSubmit_WarningDistinguishesKeyHost(t *testing.T) {
	f := newFixture(t, nil)
	cred, err := credentials.Open(f.dataDir)
	if err != nil {
		t.Fatalf("开凭证库失败：%v", err)
	}
	f.agent.Creds = cred
	f.agent.Cfg.LLM.Provider = "openai"
	f.agent.Cfg.LLM.BaseURL = "https://api.b.example.com/v1"
	f.agent.Cfg.LLM.APIKey, f.agent.Cfg.LLM.APIKeyScope = "", ""
	if err := cred.SetLLMAPIKey("sk-A-只属于A", "api.a.example.com"); err != nil {
		t.Fatalf("预置 A 家密钥失败：%v", err)
	}

	out := f.call("POST", "/api/goals", map[string]any{"goal": "换厂商之后的提交", "mode": "auto"})
	warn, _ := out["warning"].(string)
	if !strings.Contains(warn, "api.a.example.com") {
		t.Errorf("提示应点出密钥属于哪台主机，实得 %q", warn)
	}
	if strings.Contains(warn, "未配置 API Key") {
		t.Errorf("本机明明存着一把 key，不该报「未配置」：%q", warn)
	}
	id, _ := out["task_id"].(string)
	if id == "" {
		t.Fatalf("submit = %v", out)
	}
	f.waitTask(id, 10*time.Second)
}

func TestGoalGet_RejectsPathTraversal(t *testing.T) {
	srv, dataDir := newArchiveFixture(t)
	decoy := filepath.Join(dataDir, "settings.json")
	if err := os.WriteFile(decoy, []byte(`{"api_key":"不该被读到"}`), 0o644); err != nil {
		t.Fatal(err)
	}

	for _, id := range []string{"../settings", `..\settings`, "..settings", "/etc/passwd", ""} {
		if _, ok := srv.archivedTask(id); ok {
			t.Errorf("archivedTask(%q) 不该读得到 tasks/ 之外的文件", id)
		}
	}

	ts := newTokenTestServer(srv)
	defer ts.Close()
	for _, id := range []string{"..%2Fsettings", "..%5Csettings", "..settings"} {
		resp, err := http.Get(ts.URL + "/api/goals/" + id)
		if err != nil {
			t.Fatal(err)
		}
		body, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		if resp.StatusCode == http.StatusOK || strings.Contains(string(body), "api_key") {
			t.Errorf("id=%q 不该读到 tasks/ 之外的文件：状态 %d，响应 %s", id, resp.StatusCode, body)
		}
	}
}

func TestGoalList_IncludesArchived(t *testing.T) {
	srv, dataDir := newArchiveFixture(t)
	writeArchive(t, dataDir, "task-list-1", types.GoalResult{
		TaskID:   "task-list-1",
		Goal:     "昨晚跑的那个",
		Status:   types.GoalSuccess,
		Mode:     "auto",
		TaskMode: types.TaskWork,
		Score:    95,
	})
	srv.mu.Lock()
	srv.tasks["task-live"] = &taskInfo{ID: "task-live", Goal: "正在跑的", Status: types.GoalRunning}
	srv.mu.Unlock()

	ts := newTokenTestServer(srv)
	defer ts.Close()
	resp, err := http.Get(ts.URL + "/api/goals")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	var got struct {
		Count int        `json:"count"`
		Goals []taskInfo `json:"goals"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&got); err != nil {
		t.Fatal(err)
	}
	byID := map[string]taskInfo{}
	for _, g := range got.Goals {
		byID[g.ID] = g
	}
	if got.Count != 2 || len(byID) != 2 {
		t.Fatalf("应看到归档那条 + 内存那条，实得 %v", got.Goals)
	}
	arch, ok := byID["task-list-1"]
	if !ok {
		t.Fatalf("列表少了归档记录：%v", got.Goals)
	}
	if arch.Goal != "昨晚跑的那个" || arch.Status != types.GoalSuccess {
		t.Errorf("归档记录内容不对：%+v", arch)
	}
	if arch.Mode != "auto" || arch.TaskMode != string(types.TaskWork) {
		t.Errorf("归档应带出模式，实得 mode=%q task_mode=%q", arch.Mode, arch.TaskMode)
	}
	if live, ok := byID["task-live"]; !ok || live.Status != types.GoalRunning {
		t.Errorf("内存里正在跑的那条要在，且不被档案覆盖：%+v", live)
	}
}

func TestGoalSubmit_ArchivesRunMode(t *testing.T) {
	f := newFixture(t, []llm.Scripted{
		{Kind: "plan", Texts: []string{`{"steps":[{"id":"s1","description":"回复","tool":"reply","args":{"text":"模式留档"}}]}`}},
		{Kind: "reflect", Texts: []string{`{"score":90,"verdict":"done","reason":"ok"}`}},
	})
	out := f.call("POST", "/api/goals", map[string]any{"goal": "留模式", "mode": "auto", "task_mode": "code"})
	id, _ := out["task_id"].(string)
	if done := f.waitTask(id, 10*time.Second); done["status"] != "success" {
		t.Fatalf("任务没跑成：%v", done)
	}
	g, err := agent.ReadTaskResult(f.dataDir, id)
	if err != nil || g == nil {
		t.Fatalf("没归档：%-v %v", g, err)
	}
	if g.Mode != "auto" || g.TaskMode != types.TaskCode {
		t.Errorf("归档应带出跑法，实得 mode=%q task_mode=%q", g.Mode, g.TaskMode)
	}
}
