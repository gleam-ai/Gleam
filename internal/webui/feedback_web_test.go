package webui

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"gleam/internal/buildinfo"
	"gleam/internal/harness/credentials"
	"gleam/internal/harness/feedback"
	"gleam/pkg/types"
)

var pngBytes = append([]byte{0x89, 'P', 'N', 'G', '\r', '\n', 0x1a, '\n'}, bytes.Repeat([]byte{0x20}, 32)...)

func fbDo(t *testing.T, ts *httptest.Server, method, path string, body any) (int, map[string]any, []byte) {
	t.Helper()
	var rd io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			t.Fatal(err)
		}
		rd = bytes.NewReader(b)
	}
	req, err := http.NewRequest(method, ts.URL+path, rd)
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	var out map[string]any
	_ = json.Unmarshal(raw, &out)
	return resp.StatusCode, out, raw
}

func feedbackDir(dataDir string) string { return filepath.Join(dataDir, "feedback") }

func TestFeedbackSubmit_ArchivesLocally(t *testing.T) {
	srv, dataDir := newArchiveFixture(t)
	ts := newTokenTestServer(srv)
	defer ts.Close()

	code, got, _ := fbDo(t, ts, "POST", "/api/feedback", map[string]any{
		"kind": "bug", "text": "点击技能卡片的安装按钮没有反应",
	})
	if code != 201 {
		t.Fatalf("状态 = %d, want 201", code)
	}
	id, _ := got["id"].(string)
	if id == "" {
		t.Fatalf("没有返回 id：%v", got)
	}
	if got["delivery"] != string(types.FeedbackLocalOnly) {
		t.Errorf("没配远端时投递状态应为 %q（不是 failed：那会让人以为提交失败），实得 %v",
			types.FeedbackLocalOnly, got["delivery"])
	}
	b, err := os.ReadFile(filepath.Join(feedbackDir(dataDir), id+".json"))
	if err != nil {
		t.Fatalf("盘上没有归档：%v", err)
	}
	var onDisk types.Feedback
	if err := json.Unmarshal(b, &onDisk); err != nil {
		t.Fatal(err)
	}
	if onDisk.Text == "" || onDisk.Kind != types.FeedbackBug {
		t.Errorf("归档内容不对：%+v", onDisk)
	}

	code, list, _ := fbDo(t, ts, "GET", "/api/feedback", nil)
	if code != 200 {
		t.Fatalf("列表状态 = %d", code)
	}
	if n, _ := list["count"].(float64); int(n) != 1 {
		t.Errorf("列表应有 1 条，实得 %v", list["count"])
	}
}

func TestFeedbackList_SaysWhereItGoes(t *testing.T) {
	srv, _ := newArchiveFixture(t)
	ts := newTokenTestServer(srv)
	defer ts.Close()
	_, list, _ := fbDo(t, ts, "GET", "/api/feedback", nil)
	if r, _ := list["remote"].(string); r != "" {
		t.Errorf("没配远端时 remote 应为空串（界面据此显示「只存本机」），实得 %q", r)
	}

	counts, _ := countHandler()
	dsrv, _ := newDeliveryFixture(t, counts)
	dts := newTokenTestServer(dsrv)
	defer dts.Close()
	_, dl, _ := fbDo(t, dts, "GET", "/api/feedback", nil)
	if r, _ := dl["remote"].(string); r != "supabase" {
		t.Errorf("配了远端时 remote 应为投递器名，实得 %q", r)
	}
}

func TestFeedbackSubmit_ContextIsServerSideAndHostOnly(t *testing.T) {
	srv, dataDir := newArchiveFixture(t)
	const leakyToken = "sk-leak-in-path-4f9a"
	srv.Agent.Cfg.LLM.Model = "glm-4-plus"
	srv.Agent.Cfg.LLM.BaseURL = "https://gw.example.com/v1/chat/" + leakyToken
	store, err := credentials.Open(dataDir)
	if err != nil {
		t.Fatal(err)
	}
	const apiKey = "sk-real-key-7b3c9d2e"
	if err := store.SetLLMAPIKey(apiKey, "gw.example.com"); err != nil {
		t.Fatal(err)
	}
	ts := newTokenTestServer(srv)
	defer ts.Close()

	code, got, _ := fbDo(t, ts, "POST", "/api/feedback", map[string]any{
		"kind":    "suggestion",
		"text":    "希望技能能批量启用",
		"context": map[string]any{"app_version": "9.9.9", "llm_host": "attacker.example.com", "api_key": apiKey},
	})
	if code != 201 {
		t.Fatalf("状态 = %d", code)
	}
	ctxv, _ := got["context"].(map[string]any)
	if ctxv == nil {
		t.Fatalf("没有 context：%v", got)
	}
	if ctxv["app_version"] != buildinfo.Version {
		t.Errorf("app_version 应为后端真实版本 %q，实得 %v（前端的 context 被采信了）", buildinfo.Version, ctxv["app_version"])
	}
	if ctxv["llm_host"] != "gw.example.com" {
		t.Errorf("llm_host 应只有主机名，实得 %v", ctxv["llm_host"])
	}
	if ctxv["model"] != "glm-4-plus" {
		t.Errorf("model = %v, want glm-4-plus", ctxv["model"])
	}

	id, _ := got["id"].(string)
	b, err := os.ReadFile(filepath.Join(feedbackDir(dataDir), id+".json"))
	if err != nil {
		t.Fatal(err)
	}
	for _, forbidden := range []string{apiKey, leakyToken} {
		if strings.Contains(string(b), forbidden) {
			t.Errorf("归档里出现了不该外发的片段 %q：%s", forbidden, b)
		}
	}
}

func TestFeedbackSubmit_ContextFromTaskArchive(t *testing.T) {
	srv, dataDir := newArchiveFixture(t)
	writeArchive(t, dataDir, "task-fb-1", types.GoalResult{
		TaskID: "task-fb-1",
		Goal:   "把报告发到群里",
		Status: types.GoalFailed,
		Steps: []types.StepResult{
			{StepID: "s1", Tool: "web.search", Status: types.StepSucceeded},
			{StepID: "s2", Tool: "file.write", Status: types.StepFailed, Error: "权限不足"},
			{StepID: "s3", Tool: "http.post", Status: types.StepFailed, Error: "连接被重置"},
		},
	})
	ts := newTokenTestServer(srv)
	defer ts.Close()

	_, got, _ := fbDo(t, ts, "POST", "/api/feedback", map[string]any{
		"kind": "bug", "text": "发送环节一直失败", "task_id": "task-fb-1",
	})
	ctxv, _ := got["context"].(map[string]any)
	if ctxv["task_id"] != "task-fb-1" {
		t.Errorf("task_id = %v, want task-fb-1", ctxv["task_id"])
	}
	if ctxv["task_status"] != string(types.GoalFailed) {
		t.Errorf("task_status = %v, want failed", ctxv["task_status"])
	}
	if ctxv["failed_tool"] != "http.post" {
		t.Errorf("failed_tool 应为最后一个失败步骤 %q，实得 %v", "http.post", ctxv["failed_tool"])
	}
	id, _ := got["id"].(string)
	b, err := os.ReadFile(filepath.Join(feedbackDir(dataDir), id+".json"))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(b), "连接被重置") {
		t.Errorf("归档里混进了步骤错误正文（只该带工具名）：%s", b)
	}
}

func TestFeedbackSubmit_RejectsBadInput(t *testing.T) {
	srv, dataDir := newArchiveFixture(t)
	ts := newTokenTestServer(srv)
	defer ts.Close()

	cases := []struct {
		name string
		body map[string]any
	}{
		{"类型不认识", map[string]any{"kind": "complaint", "text": "有点慢"}},
		{"类型缺失", map[string]any{"text": "有点慢"}},
		{"描述为空", map[string]any{"kind": "bug", "text": "   "}},
		{"描述超长", map[string]any{"kind": "bug", "text": strings.Repeat("字", maxFeedbackTextRunes+1)}},
	}
	for _, c := range cases {
		code, out, _ := fbDo(t, ts, "POST", "/api/feedback", c.body)
		if code != 400 {
			t.Errorf("%s：状态 = %d（want 400），响应 %v", c.name, code, out)
		}
	}
	entries, err := os.ReadDir(feedbackDir(dataDir))
	if err != nil && !os.IsNotExist(err) {
		t.Fatal(err)
	}
	if len(entries) != 0 {
		t.Errorf("被拒的提交不该留下文件，实得 %d 个", len(entries))
	}
}

func TestFeedbackSubmit_StoresAndServesScreenshot(t *testing.T) {
	srv, _ := newArchiveFixture(t)
	ts := newTokenTestServer(srv)
	defer ts.Close()

	dataURL := "data:image/png;base64," + base64.StdEncoding.EncodeToString(pngBytes)
	code, got, _ := fbDo(t, ts, "POST", "/api/feedback", map[string]any{
		"kind":        "bug",
		"text":        "截图里是报错弹窗",
		"attachments": []any{map[string]any{"data": dataURL}},
	})
	if code != 201 {
		t.Fatalf("状态 = %d, want 201", code)
	}
	atts, _ := got["attachments"].([]any)
	if len(atts) != 1 {
		t.Fatalf("attachments = %v", got["attachments"])
	}
	a, _ := atts[0].(map[string]any)
	name, _ := a["name"].(string)
	if !strings.HasSuffix(name, ".png") {
		t.Errorf("文件名应以 sniff 出的扩展名结尾，实得 %q", name)
	}
	if mime, _ := a["mime"].(string); mime != "image/png" {
		t.Errorf("mime = %v, want image/png", a["mime"])
	}

	resp, err := http.Get(ts.URL + "/api/feedback/attachment?name=" + name)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != 200 {
		t.Fatalf("取回截图状态 = %d", resp.StatusCode)
	}
	if resp.Header.Get("Content-Type") != "image/png" {
		t.Errorf("Content-Type = %q", resp.Header.Get("Content-Type"))
	}
	if !bytes.Equal(body, pngBytes) {
		t.Error("取回的字节与原图不一致")
	}
}

func TestFeedbackSubmit_FakeImageRollsBack(t *testing.T) {
	srv, dataDir := newArchiveFixture(t)
	ts := newTokenTestServer(srv)
	defer ts.Close()

	script := base64.StdEncoding.EncodeToString([]byte("#!/bin/sh\nrm -rf /\n"))
	png := "data:image/png;base64," + base64.StdEncoding.EncodeToString(pngBytes)
	code, out, _ := fbDo(t, ts, "POST", "/api/feedback", map[string]any{
		"kind": "bug",
		"text": "第一张能存、第二张是伪装成图片的脚本",
		"attachments": []any{
			map[string]any{"data": png},
			map[string]any{"data": script},
		},
	})
	if code != 400 {
		t.Fatalf("伪装图片应回 400，实得 %d：%v", code, out)
	}
	many := make([]any, 0, maxFeedbackAttachments+1)
	for i := 0; i <= maxFeedbackAttachments; i++ {
		many = append(many, map[string]any{"data": png})
	}
	if code, out, _ := fbDo(t, ts, "POST", "/api/feedback", map[string]any{
		"kind": "bug", "text": "一次贴太多", "attachments": many,
	}); code != 400 {
		t.Fatalf("超张数应回 400，实得 %d：%v", code, out)
	}
	entries, err := os.ReadDir(feedbackDir(dataDir))
	if err != nil && !os.IsNotExist(err) {
		t.Fatal(err)
	}
	if len(entries) != 0 {
		t.Errorf("被拒的提交留下了 %d 个文件：%v", len(entries), entries)
	}
}

func TestFeedbackAttachment_RejectsTraversal(t *testing.T) {
	srv, dataDir := newArchiveFixture(t)
	decoy := filepath.Join(dataDir, "settings.json")
	if err := os.WriteFile(decoy, []byte(`{"api_key":"不该被读到"}`), 0o644); err != nil {
		t.Fatal(err)
	}
	ts := newTokenTestServer(srv)
	defer ts.Close()

	for _, name := range []string{"../settings.json", `..\..\settings.json`, "settings.json", "", "fb-99999999-999999-ffff-1.png", "fb-20260924-120000-abcd-1.exe"} {
		resp, err := http.Get(ts.URL + "/api/feedback/attachment?name=" + name)
		if err != nil {
			t.Fatal(err)
		}
		body, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		if resp.StatusCode == http.StatusOK || strings.Contains(string(body), "api_key") {
			t.Errorf("name=%q 不该读到 feedback/ 之外的文件：状态 %d，响应 %s", name, resp.StatusCode, body)
		}
	}
	if _, err := os.Stat(decoy); err != nil {
		t.Errorf("诱饵文件被动了：%v", err)
	}
}

func TestFeedbackDelete_RemovesArchiveAndScreenshots(t *testing.T) {
	srv, dataDir := newArchiveFixture(t)
	ts := newTokenTestServer(srv)
	defer ts.Close()

	_, got, _ := fbDo(t, ts, "POST", "/api/feedback", map[string]any{
		"kind":        "bug",
		"text":        "删掉这条，别留截图",
		"attachments": []any{map[string]any{"data": base64.StdEncoding.EncodeToString(pngBytes)}},
	})
	id, _ := got["id"].(string)
	if id == "" {
		t.Fatal("没拿到 id")
	}
	atts, _ := got["attachments"].([]any)
	if len(atts) != 1 {
		t.Fatalf("截图没存上：%v", got["attachments"])
	}

	if code, out, _ := fbDo(t, ts, "DELETE", "/api/feedback/"+id, nil); code != 200 {
		t.Fatalf("删除状态 = %d：%v", code, out)
	}
	left, _ := filepath.Glob(filepath.Join(feedbackDir(dataDir), id+"*"))
	if len(left) != 0 {
		t.Errorf("删除后仍有残留：%v", left)
	}
	if code, _, _ := fbDo(t, ts, "DELETE", "/api/feedback/"+id, nil); code != 404 {
		t.Errorf("重复删除应回 404，实得 %d", code)
	}
	if code, _, _ := fbDo(t, ts, "DELETE", "/api/feedback/not-a-real-id", nil); code != 404 {
		t.Errorf("形状不合的 id 应回 404，实得 %d", code)
	}
}

const secretWorkspace = `C:\Users\宁\Projects\不该出去的项目`
const secretAPIKey = "sk-e2e-key-9d2f4b7a"

func newDeliveryFixture(t *testing.T, handler http.HandlerFunc) (*Server, string) {
	t.Helper()
	srv, dataDir := newArchiveFixture(t)
	remote := httptest.NewServer(handler)
	t.Cleanup(remote.Close)
	creds, err := credentials.Open(dataDir)
	if err != nil {
		t.Fatal(err)
	}
	if err := creds.SetCloudConfig(credentials.CloudConfig{SupabaseURL: remote.URL, SupabaseAnonKey: "anon"}); err != nil {
		t.Fatal(err)
	}
	if err := creds.SetLLMAPIKey(secretAPIKey, "open.bigmodel.cn"); err != nil {
		t.Fatal(err)
	}
	srv.Agent.Creds = creds
	srv.Agent.Cfg.Workspace = secretWorkspace
	srv.Agent.Cfg.LLM.BaseURL = "https://open.bigmodel.cn/api/paas/v4"
	srv.Agent.Cfg.LLM.Model = "glm-4-plus"
	return srv, dataDir
}

func countHandler() (http.HandlerFunc, func() int) {
	var mu sync.Mutex
	n := 0
	return func(w http.ResponseWriter, r *http.Request) {
			mu.Lock()
			n++
			mu.Unlock()
			w.WriteHeader(http.StatusCreated)
		}, func() int {
			mu.Lock()
			defer mu.Unlock()
			return n
		}
}

func TestFeedbackSubmit_DeliversRedactedCopy(t *testing.T) {
	var mu sync.Mutex
	var bodies []map[string]any
	handler := func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		var row map[string]any
		_ = json.Unmarshal(b, &row)
		mu.Lock()
		bodies = append(bodies, row)
		mu.Unlock()
		w.WriteHeader(http.StatusCreated)
	}
	srv, dataDir := newDeliveryFixture(t, handler)
	ts := newTokenTestServer(srv)
	defer ts.Close()

	problemText := `写入失败：cannot open ` + secretWorkspace + `\a.txt（key=` + secretAPIKey + `）`
	_, got, _ := fbDo(t, ts, "POST", "/api/feedback", map[string]any{
		"kind":        "bug",
		"text":        problemText,
		"attachments": []any{map[string]any{"data": "data:image/png;base64," + base64.StdEncoding.EncodeToString(pngBytes)}},
	})
	if got["delivery"] != string(types.FeedbackSent) {
		t.Fatalf("配了远端且送成功，状态应为 sent，实得 %v（note=%v）", got["delivery"], got["delivery_note"])
	}
	note, _ := got["delivery_note"].(string)
	if !strings.Contains(note, "替换了 2 处") {
		t.Errorf("投递备注该说明动过几处机密片段（用户有权知道发出去的和写下的不一样），实得 %q", note)
	}

	mu.Lock()
	defer mu.Unlock()
	if len(bodies) != 1 {
		t.Fatalf("远端收到 %d 个请求", len(bodies))
	}
	raw, _ := json.Marshal(bodies[0])
	for _, forbidden := range []string{secretWorkspace, secretAPIKey, "iVBOR"} {
		if strings.Contains(string(raw), forbidden) {
			t.Errorf("发出去的那份里有不该出去的东西 %q：%s", forbidden, raw)
		}
	}
	sent, _ := bodies[0]["text"].(string)
	if !strings.Contains(sent, feedback.RedactedMarker) {
		t.Errorf("外发副本该留脱敏记号：%s", sent)
	}
	if bodies[0]["llm_host"] != "open.bigmodel.cn" || bodies[0]["app_version"] != buildinfo.Version {
		t.Errorf("现场列没带上或不对：%v / %v", bodies[0]["llm_host"], bodies[0]["app_version"])
	}

	b, err := os.ReadFile(filepath.Join(feedbackDir(dataDir), got["id"].(string)+".json"))
	if err != nil {
		t.Fatal(err)
	}
	var onDisk types.Feedback
	if err := json.Unmarshal(b, &onDisk); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(onDisk.Text, secretWorkspace) || !strings.Contains(onDisk.Text, secretAPIKey) {
		t.Errorf("本地归档被一起改掉了（用户复查时该看到自己写的原文）：%s", onDisk.Text)
	}
	if onDisk.Delivery != types.FeedbackSent {
		t.Errorf("投递状态没写回归档，刷新后就看不见\"已送达\"了：%+v", onDisk)
	}
}

func TestFeedbackSubmit_DeliveryFailureStaysLocalAndVisible(t *testing.T) {
	srv, dataDir := newDeliveryFixture(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = io.WriteString(w, `{"message":"gateway blew up"}`)
	})
	ts := newTokenTestServer(srv)
	defer ts.Close()

	code, got, _ := fbDo(t, ts, "POST", "/api/feedback", map[string]any{"kind": "bug", "text": "远端是坏的"})
	if code != 201 {
		t.Fatalf("投递失败不该让提交失败，状态 = %d：%v", code, got)
	}
	if got["delivery"] != string(types.FeedbackFailed) {
		t.Errorf("状态应为 failed，实得 %v", got["delivery"])
	}
	note, _ := got["delivery_note"].(string)
	if !strings.Contains(note, "未送达") || !strings.Contains(note, "500") {
		t.Errorf("备注该说清是没送到、为什么，实得 %q", note)
	}
	b, err := os.ReadFile(filepath.Join(feedbackDir(dataDir), got["id"].(string)+".json"))
	if err != nil {
		t.Fatal(err)
	}
	var onDisk types.Feedback
	if err := json.Unmarshal(b, &onDisk); err != nil {
		t.Fatal(err)
	}
	if onDisk.Delivery != types.FeedbackFailed || onDisk.DeliveryNote == "" {
		t.Errorf("归档里的投递状态不对：%+v", onDisk)
	}
}

func TestFeedbackResend_DoesNotRepeatSentRows(t *testing.T) {
	counts, sent := countHandler()
	srv, _ := newDeliveryFixture(t, counts)
	ts := newTokenTestServer(srv)
	defer ts.Close()

	_, got, _ := fbDo(t, ts, "POST", "/api/feedback", map[string]any{"kind": "suggestion", "text": "希望支持批量启用"})
	id, _ := got["id"].(string)
	if sent() != 1 {
		t.Fatalf("提交时应投递一次，实得 %d 次", sent())
	}
	_, resent, _ := fbDo(t, ts, "POST", "/api/feedback/"+id+"/resend", nil)
	if got2, _ := resent["delivery"].(string); got2 != string(types.FeedbackSent) {
		t.Errorf("重投一条已送达的反馈后状态应仍是 sent，实得 %v", got2)
	}
	if n := sent(); n != 1 {
		t.Errorf("已送达的不该再送一次，远端收到 %d 个请求", n)
	}
	srv.Agent.Creds = nil
	if code, out, _ := fbDo(t, ts, "POST", "/api/feedback/"+id+"/resend", nil); code != 400 {
		t.Errorf("没配远端应回 400，实得 %d：%v", code, out)
	}
	if code, _, _ := fbDo(t, ts, "POST", "/api/feedback/no-such-id/resend", nil); code != 404 {
		t.Errorf("没有这条反馈应回 404，实得 %d", code)
	}
}

func TestFeedbackContextPreview_MatchesWhatGetsArchived(t *testing.T) {
	counts, _ := countHandler()
	srv, dataDir := newDeliveryFixture(t, counts)
	writeArchive(t, dataDir, "task-fb-prev", types.GoalResult{
		TaskID: "task-fb-prev", Goal: "导出报表", Status: types.GoalFailed,
		Steps: []types.StepResult{{StepID: "s1", Tool: "shell.exec", Status: types.StepFailed, Error: "退出码 1"}},
	})
	ts := newTokenTestServer(srv)
	defer ts.Close()

	code, prev, raw := fbDo(t, ts, "GET", "/api/feedback/context?task_id=task-fb-prev", nil)
	if code != 200 {
		t.Fatalf("预览状态 = %d：%s", code, raw)
	}
	if strings.Contains(string(raw), secretWorkspace) || strings.Contains(string(raw), secretAPIKey) {
		t.Errorf("预览里出现了不该出去的东西：%s", raw)
	}
	if prev["remote"] != "supabase" {
		t.Errorf("预览应说出远端是 supabase，实得 %v", prev["remote"])
	}
	want, _ := prev["context"].(map[string]any)
	if want["failed_tool"] != "shell.exec" || want["task_status"] != string(types.GoalFailed) {
		t.Errorf("预览字段不对：%v", want)
	}

	_, sent, _ := fbDo(t, ts, "POST", "/api/feedback", map[string]any{
		"kind": "bug", "text": "导出总是失败", "task_id": "task-fb-prev",
	})
	got, _ := sent["context"].(map[string]any)
	if fmt.Sprint(got) != fmt.Sprint(want) {
		t.Errorf("预览与实存不一致：预览 %v，实存 %v", want, got)
	}
}
