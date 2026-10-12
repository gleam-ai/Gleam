package webui

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"
)

func (f *fixture) raw(method, path string, body any) (int, map[string]any) {
	f.t.Helper()
	var rdr *strings.Reader
	if body != nil {
		b, _ := json.Marshal(body)
		rdr = strings.NewReader(string(b))
	} else {
		rdr = strings.NewReader("")
	}
	req, err := http.NewRequest(method, f.ts.URL+path, rdr)
	if err != nil {
		f.t.Fatal(err)
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		f.t.Fatal(err)
	}
	defer resp.Body.Close()
	var out map[string]any
	_ = json.NewDecoder(resp.Body).Decode(&out)
	return resp.StatusCode, out
}

func TestWebUI_ScheduleNotify_DefaultsPrinted(t *testing.T) {
	f := newFixture(t, nil)
	defer f.call("DELETE", "/api/schedules/nj-default", nil)

	created := f.call("POST", "/api/schedules", map[string]any{
		"name": "nj-default", "goal": "检查磁盘占用", "cron": "0 9 * * *",
	})
	if got := created["notify"]; got != "on_failure" {
		t.Errorf("未指定策略时应印默认 on_failure，实际 %v", got)
	}

	list := f.call("GET", "/api/schedules", nil)
	found := false
	for _, it := range list["jobs"].([]any) {
		j := it.(map[string]any)
		if j["name"] == "nj-default" {
			found = true
			if j["notify"] != "on_failure" {
				t.Errorf("列表里也应印生效策略，实际 %v", j["notify"])
			}
		}
	}
	if !found {
		t.Error("列表里应能看到刚建的任务")
	}
}

func TestWebUI_ScheduleNotify_SetAndReset(t *testing.T) {
	f := newFixture(t, nil)
	defer f.call("DELETE", "/api/schedules/nj-set", nil)
	f.call("POST", "/api/schedules", map[string]any{
		"name": "nj-set", "goal": "检查磁盘占用", "cron": "0 9 * * *",
	})

	got := f.call("POST", "/api/schedules/nj-set/notify", map[string]any{"notify": "never"})
	if got["notify"] != "never" {
		t.Errorf("改策略后应返回 never，实际 %v", got["notify"])
	}
	list := f.call("GET", "/api/schedules", nil)
	for _, it := range list["jobs"].([]any) {
		if j := it.(map[string]any); j["name"] == "nj-set" && j["notify"] != "never" {
			t.Errorf("列表里应看到 never，实际 %v", j["notify"])
		}
	}

	got = f.call("POST", "/api/schedules/nj-set/notify", map[string]any{"notify": ""})
	if got["notify"] != "on_failure" {
		t.Errorf("空串应恢复默认 on_failure，实际 %v", got["notify"])
	}
}

func TestWebUI_ScheduleNotify_InvalidRejected(t *testing.T) {
	f := newFixture(t, nil)

	code, out := f.raw("POST", "/api/schedules", map[string]any{
		"name": "nj-bad", "goal": "检查磁盘占用", "cron": "0 9 * * *", "notify": "yelling",
	})
	if code != 400 {
		t.Errorf("非法策略应返回 400，实际 %d（%v）", code, out)
	}
	msg, _ := out["error"].(string)
	for _, want := range []string{"always", "on_failure", "never"} {
		if !strings.Contains(msg, want) {
			t.Errorf("报错应列出可用值（缺 %q）：%s", want, msg)
		}
	}

	code, _ = f.raw("POST", "/api/schedules/nj-bad/notify", map[string]any{"notify": "never"})
	if code == 200 {
		t.Error("非法策略创建失败后，不该留下同名任务（先校验再创建的判据被破坏了）")
	}
}

func TestWebUI_ScheduleEnabled_PauseResume(t *testing.T) {
	f := newFixture(t, nil)
	defer f.call("DELETE", "/api/schedules/en-job", nil)
	created := f.call("POST", "/api/schedules", map[string]any{
		"name": "en-job", "goal": "检查磁盘占用", "cron": "0 9 * * *",
	})
	if created["enabled"] != true {
		t.Fatalf("新建任务应默认启用，实际 %v", created["enabled"])
	}
	got := f.call("POST", "/api/schedules/en-job/enabled", map[string]any{"enabled": false})
	if got["enabled"] != false {
		t.Errorf("暂停后应返回 enabled=false，实际 %v", got["enabled"])
	}
	list := f.call("GET", "/api/schedules", nil)
	for _, it := range list["jobs"].([]any) {
		if j := it.(map[string]any); j["name"] == "en-job" && j["enabled"] != false {
			t.Errorf("列表里应看到停用状态，实际 %v", j["enabled"])
		}
	}
	if got = f.call("POST", "/api/schedules/en-job/enabled", map[string]any{"enabled": true}); got["enabled"] != true {
		t.Errorf("恢复后应返回 enabled=true，实际 %v", got["enabled"])
	}
	if code, _ := f.raw("POST", "/api/schedules/en-job/enabled", map[string]any{}); code != 400 {
		t.Errorf("缺 enabled 字段应 400，实际 %d", code)
	}
	if code, _ := f.raw("POST", "/api/schedules/en-nope/enabled", map[string]any{"enabled": false}); code != 400 {
		t.Errorf("不存在的任务应 400，实际 %d", code)
	}
}

func TestWebUI_ScheduleNotify_MissingJobRejected(t *testing.T) {
	f := newFixture(t, nil)
	code, out := f.raw("POST", "/api/schedules/never-existed/notify", map[string]any{"notify": "never"})
	if code != 400 {
		t.Errorf("不存在的任务应返回 400，实际 %d（%v）", code, out)
	}
	if msg, _ := out["error"].(string); !strings.Contains(msg, "never-existed") {
		t.Errorf("报错应点出是哪个任务：%v", out)
	}
}
