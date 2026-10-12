package scheduler

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestShouldNotify_PolicyMatrix(t *testing.T) {
	cases := []struct {
		name      string
		policy    string
		succeeded bool
		want      bool
	}{
		{"默认·失败要响", "", false, true},
		{"always·失败要响", NotifyAlways, false, true},
		{"on_failure·失败要响", NotifyOnFailure, false, true},
		{"脏值·失败仍然要响", "sometimes", false, true},
		{"默认·成功静默", "", true, false},
		{"on_failure·成功静默", NotifyOnFailure, true, false},
		{"never·成功静默", NotifyNever, true, false},
		{"never·失败也静默（用户显式要求）", NotifyNever, false, false},
		{"脏值·成功静默", "sometimes", true, false},
		{"always·成功也响", NotifyAlways, true, true},
		{"带空白仍然认得出", "  always  ", true, true},
	}
	for _, c := range cases {
		j := Job{Name: "j", Notify: c.policy}
		if got := j.ShouldNotify(c.succeeded); got != c.want {
			t.Errorf("%s: ShouldNotify(%v) = %v，应为 %v", c.name, c.succeeded, got, c.want)
		}
	}
}

func TestNormalizeNotify(t *testing.T) {
	ok := map[string]string{
		"":              NotifyOnFailure,
		"   ":           NotifyOnFailure,
		"always":        NotifyAlways,
		"on_failure":    NotifyOnFailure,
		"never":         NotifyNever,
		"  always\t":    NotifyAlways,
		" on_failure  ": NotifyOnFailure,
	}
	for in, want := range ok {
		got, err := NormalizeNotify(in)
		if err != nil || got != want {
			t.Errorf("NormalizeNotify(%q) = (%q, %v)，应为 %q", in, got, err, want)
		}
	}
	for _, bad := range []string{"sometimes", "ALWAYS", "0", "true"} {
		if _, err := NormalizeNotify(bad); err == nil {
			t.Errorf("NormalizeNotify(%q) 应报错", bad)
		}
	}
}

func TestSetNotify_PersistsExplicitPolicyOnly(t *testing.T) {
	path := filepath.Join(t.TempDir(), "schedules.json")
	s, err := Open(path, nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.AddJob("j", "0 9 * * *", 0, "巡检", "auto"); err != nil {
		t.Fatal(err)
	}

	if err := s.SetNotify("j", NotifyAlways); err != nil {
		t.Fatal(err)
	}
	if err := s.SetNotify("j", NotifyNever); err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(raw), `"notify": "never"`) && !strings.Contains(string(raw), `"notify":"never"`) {
		t.Errorf("显式策略应落盘，实际文件内容：%s", raw)
	}

	if err := s.SetNotify("j", ""); err != nil {
		t.Fatal(err)
	}
	raw, _ = os.ReadFile(path)
	if strings.Contains(string(raw), "notify") {
		t.Errorf("默认策略不该落盘，实际文件内容：%s", raw)
	}

	if err := s.SetNotify("j", NotifyAlways); err != nil {
		t.Fatal(err)
	}
	if err := s.SetNotify("j", "sometimes"); err == nil {
		t.Error("非法策略应报错")
	}
	var jobs []Job
	b, _ := os.ReadFile(path)
	if err := json.Unmarshal(b, &jobs); err != nil {
		t.Fatalf("读回调度文件失败：%v", err)
	}
	if len(jobs) != 1 || jobs[0].Notify != NotifyAlways {
		t.Errorf("非法策略不该改掉已有设置，实际：%+v", jobs)
	}
}

func TestSetNotify_MissingJob(t *testing.T) {
	s, err := Open(filepath.Join(t.TempDir(), "schedules.json"), nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.SetNotify("nope", NotifyAlways); err == nil {
		t.Error("任务不存在时应报错")
	}
	if err := s.SetNotify("nope", "sometimes"); err == nil {
		t.Error("非法策略应报错（即使任务不存在）")
	}
}
