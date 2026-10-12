package agent

import (
	"strings"
	"testing"
	"time"

	"gleam/internal/harness/scheduler"
)

func TestJobToScheduleView_PrintsEffectivePolicy(t *testing.T) {
	cases := []struct {
		stored string
		want   string
	}{
		{"", scheduler.NotifyOnFailure},
		{scheduler.NotifyAlways, scheduler.NotifyAlways},
		{scheduler.NotifyNever, scheduler.NotifyNever},
		{scheduler.NotifyOnFailure, scheduler.NotifyOnFailure},
	}
	for _, c := range cases {
		got := jobToScheduleView(scheduler.Job{Name: "j", Notify: c.stored}).Notify
		if got != c.want {
			t.Errorf("存储值 %q 应印成 %q，实际 %q", c.stored, c.want, got)
		}
	}
}

func TestJobToScheduleView_DirtyStoredValuePrintedAsIs(t *testing.T) {
	got := jobToScheduleView(scheduler.Job{Name: "j", Notify: "shouty"}).Notify
	if got != "shouty" {
		t.Errorf("脏值应原样印出（好让用户发现配置坏了），实际 %q", got)
	}
}

func TestJobToScheduleView_KeepsScheduleFields(t *testing.T) {
	next := time.Now().Add(time.Hour)
	v := jobToScheduleView(scheduler.Job{
		Name: "巡检", Cron: "0 9 * * *", Goal: "检查磁盘占用", Mode: "auto",
		Enabled: true, WhenText: "每天9点", ScheduleText: "每天 09:00",
		NextRun: &next,
	})
	for _, c := range []struct{ field, got, want string }{
		{"Name", v.Name, "巡检"},
		{"Cron", v.Cron, "0 9 * * *"},
		{"Goal", v.Goal, "检查磁盘占用"},
		{"Mode", v.Mode, "auto"},
		{"WhenText", v.WhenText, "每天9点"},
		{"ScheduleText", v.ScheduleText, "每天 09:00"},
	} {
		if c.got != c.want {
			t.Errorf("视图字段 %s = %q，期望 %q", c.field, c.got, c.want)
		}
	}
	if !v.Enabled {
		t.Error("视图应带上启用状态")
	}
	if v.NextRun == "" || !strings.Contains(v.NextRun, "T") {
		t.Errorf("NextRun 应排成 RFC3339，实际 %q", v.NextRun)
	}
}
