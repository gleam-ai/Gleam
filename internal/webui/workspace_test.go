package webui

import (
	"context"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"gleam/internal/config"
	"gleam/pkg/types"
)

func TestWebUI_WorkspaceViewAndSet(t *testing.T) {
	f := newFixture(t, nil)
	oldWs := f.ws

	newWs := t.TempDir()
	out := f.call("POST", "/api/workspace", map[string]any{"path": newWs})
	if out["workspace"] != filepath.Clean(newWs) {
		t.Errorf("workspace = %v", out["workspace"])
	}
	recents := out["recents"].([]any)
	if len(recents) == 0 || recents[0] != filepath.Clean(newWs) {
		t.Errorf("recents = %v", recents)
	}
	if f.agent.Cfg.Workspace != filepath.Clean(newWs) {
		t.Error("cfg.Workspace 未更新")
	}
	if len(f.agent.FileTools.Roots) != 1 || f.agent.FileTools.Roots[0] != filepath.Clean(newWs) {
		t.Errorf("FileTools.Roots = %v", f.agent.FileTools.Roots)
	}

	noApproval := func(types.ApprovalRequest) types.ApprovalResponse {
		t.Error("工作区内路径不应触发审批")
		return types.ApprovalResponse{Approved: false}
	}
	if _, err := f.agent.DirectToolCall(context.Background(), "file.write",
		map[string]any{"path": "inside.txt", "content": "ok"}, noApproval); err != nil {
		t.Fatalf("新工作区内写入失败: %v", err)
	}
	if _, err := os.Stat(filepath.Join(newWs, "inside.txt")); err != nil {
		t.Error("文件未写入新工作区")
	}

	deny := func(types.ApprovalRequest) types.ApprovalResponse {
		return types.ApprovalResponse{Approved: false, Note: "测试拒绝"}
	}
	outside := filepath.Join(oldWs, "escape.txt")
	if _, err := f.agent.DirectToolCall(context.Background(), "file.write",
		map[string]any{"path": outside, "content": "escape"}, deny); err == nil || !strings.Contains(err.Error(), "拒绝") {
		t.Errorf("写入旧工作区应被拒绝: %v", err)
	}
	if _, err := os.Stat(filepath.Join(newWs, "inside.txt")); err != nil {
		t.Error("文件未写入新工作区")
	}

	fresh := config.Default()
	if err := config.LoadOverlay(fresh, filepath.Join(f.dataDir, config.OverlayFile)); err != nil {
		t.Fatal(err)
	}
	if fresh.Workspace != filepath.Clean(newWs) || len(fresh.WorkspaceRecents) == 0 {
		t.Errorf("覆盖层 = %s %v", fresh.Workspace, fresh.WorkspaceRecents)
	}
}

func TestWebUI_WorkspaceClear(t *testing.T) {
	f := newFixture(t, nil)
	newWs := t.TempDir()
	f.call("POST", "/api/workspace", map[string]any{"path": newWs})

	out := f.call("POST", "/api/workspace/clear", nil)
	if out["workspace"] != "" {
		t.Errorf("清空后 workspace = %v，应为空", out["workspace"])
	}
	if len(out["recents"].([]any)) == 0 {
		t.Error("清空后 recents 不应为空")
	}
	if f.agent.Cfg.Workspace != "" {
		t.Errorf("cfg.Workspace = %q，应为空", f.agent.Cfg.Workspace)
	}
	if len(f.agent.FileTools.Roots) != 0 {
		t.Errorf("FileTools.Roots = %v，应为空", f.agent.FileTools.Roots)
	}
	fresh := config.Default()
	if err := config.LoadOverlay(fresh, filepath.Join(f.dataDir, config.OverlayFile)); err != nil {
		t.Fatal(err)
	}
	if fresh.Workspace != "" {
		t.Errorf("覆盖层 workspace = %q，应为空", fresh.Workspace)
	}
}

func TestWebUI_WorkspaceInvalid(t *testing.T) {
	f := newFixture(t, nil)
	resp, err := http.Post(f.ts.URL+"/api/workspace", "application/json", strings.NewReader(`{"path":"不存在的目录xyz"}`))
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != 400 {
		t.Errorf("不存在目录应 400, got %d", resp.StatusCode)
	}
}

func TestWebUI_FsBrowse(t *testing.T) {
	f := newFixture(t, nil)
	out := f.call("GET", "/api/fs?path="+f.ws, nil)
	if _, ok := out["dirs"]; !ok {
		t.Errorf("browse = %v", out)
	}
	roots := f.call("GET", "/api/fs", nil)
	if len(roots["dirs"].([]any)) == 0 {
		t.Error("根列表不应为空")
	}
	resp, err := http.Get(f.ts.URL + "/api/fs?path=Z:/no/such/dir")
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != 400 {
		t.Errorf("不存在目录应 400, got %d", resp.StatusCode)
	}
}

func TestWebUI_SubmitWithoutKeyWarns(t *testing.T) {
	f := newFixture(t, nil)
	f.agent.Cfg.LLM.Provider = "glm"
	f.agent.Cfg.LLM.APIKey = ""
	out := f.call("POST", "/api/goals", map[string]any{"goal": "你好", "task_mode": "chat"})
	if out["warning"] == "" || !strings.Contains(out["warning"].(string), "API Key") {
		t.Errorf("应返回无 Key 警告: %v", out)
	}
	f2 := newFixture(t, nil)
	out2 := f2.call("POST", "/api/goals", map[string]any{"goal": "你好", "task_mode": "chat"})
	if _, has := out2["warning"]; has {
		t.Errorf("mock 不应告警: %v", out2)
	}
	id, _ := out["task_id"].(string)
	id2, _ := out2["task_id"].(string)
	f.waitTask(id, 10*time.Second)
	f2.waitTask(id2, 10*time.Second)
}

func TestWebUI_ToolPermissionSetAndPersist(t *testing.T) {
	f := newFixture(t, nil)
	out := f.call("POST", "/api/tools/permission", map[string]any{"name": "file.write", "permission": "readonly"})
	if out["overridden"] != true {
		t.Errorf("set = %v", out)
	}
	tools := f.call("GET", "/api/tools", nil)
	found := false
	for _, it := range tools["tools"].([]any) {
		tm := it.(map[string]any)
		if tm["name"] == "file.write" {
			found = true
			if tm["permission"] != "readonly" || tm["overridden"] != true {
				t.Errorf("shell.exec = %v", tm)
			}
		}
	}
	if !found {
		t.Fatal("列表缺少 shell.exec")
	}
	fresh := config.Default()
	if err := config.LoadOverlay(fresh, filepath.Join(f.dataDir, config.OverlayFile)); err != nil {
		t.Fatal(err)
	}
	if fresh.Safety.ToolPermissions["file.write"] != "readonly" {
		t.Errorf("覆盖层 = %v", fresh.Safety.ToolPermissions)
	}
	out2 := f.call("POST", "/api/tools/permission", map[string]any{"name": "file.write", "permission": "default"})
	if out2["overridden"] != false {
		t.Errorf("default 应清除覆盖: %v", out2)
	}
	resp, err := http.Post(f.ts.URL+"/api/tools/permission", "application/json", strings.NewReader(`{"name":"file.write","permission":"god"}`))
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != 400 {
		t.Errorf("非法权限应 400, got %d", resp.StatusCode)
	}
}
