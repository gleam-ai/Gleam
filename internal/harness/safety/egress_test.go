package safety

import (
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestRecordEgressKeepsHostAndSizeOnly(t *testing.T) {
	path := filepath.Join(t.TempDir(), "audit.jsonl")
	g := New("auto", nil, nil, nil, time.Second)
	g.SetAuditPath(path)

	g.RecordEgress("llm", "api.deepseek.com", 12345)

	entries, err := LoadAuditLog(path, 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 {
		t.Fatalf("应有 1 条留痕，实际 %d", len(entries))
	}
	e := entries[0]
	if e.Action != "egress" {
		t.Errorf("动作应为 egress，实际 %q", e.Action)
	}
	if e.Tool != "llm" {
		t.Errorf("工具应为 llm，实际 %q", e.Tool)
	}
	if e.Egress != "llm:api.deepseek.com" {
		t.Errorf("结构化目标不对，实际 %q", e.Egress)
	}
	if !strings.Contains(e.Reason, "api.deepseek.com") || !strings.Contains(e.Reason, "12345") {
		t.Errorf("说明里应含主机与字节数，实际 %q", e.Reason)
	}
	if e.Detail != "" {
		t.Errorf("出网留痕不该带明细（那是给正文留的口子），实际 %q", e.Detail)
	}
}

func TestRecordEgressDropsEmptyHost(t *testing.T) {
	path := filepath.Join(t.TempDir(), "audit.jsonl")
	g := New("auto", nil, nil, nil, time.Second)
	g.SetAuditPath(path)

	g.RecordEgress("llm", "", 100)
	g.RecordEgress("", "example.com", 100)

	entries, err := LoadAuditLog(path, 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 0 {
		t.Fatalf("主机名或类型为空时不该留痕，实际 %+v", entries)
	}
}

func TestRecordEgressNilGateSafe(t *testing.T) {
	var g *Gate
	g.RecordEgress("llm", "example.com", 1)
}

func TestEgressStatsCountsFromStructuredFields(t *testing.T) {
	g := New("auto", nil, nil, nil, time.Second)
	g.RecordEgress("llm", "api.deepseek.com", 1200)
	g.RecordEgress("llm", "api.deepseek.com", 800)
	g.RecordEgress("llm", "open.bigmodel.cn", 500)
	g.RecordEgress("web.fetch", "example.com", 60)
	g.Record(AuditEntry{Tool: "file.write", Action: "approved", Reason: "路径均在信任路径内"})

	rep := g.EgressStats()
	if rep.Scanned != 5 || rep.Cap != auditCap {
		t.Errorf("统计范围应随读数一起给出：%+v（应扫 5 条、容量 %d）", rep, auditCap)
	}
	byKind := map[string]EgressStat{}
	for _, s := range rep.Stats {
		byKind[s.Kind] = s
	}
	llmStat, ok := byKind["llm"]
	if !ok {
		t.Fatalf("台账里没有 llm 这一类：%+v", rep.Stats)
	}
	if llmStat.Count != 3 || llmStat.Bytes != 2500 {
		t.Errorf("llm 应为 3 次 / 2500 字节，实际 %+v", llmStat)
	}
	if len(llmStat.Hosts) != 2 || llmStat.Hosts[0] != "api.deepseek.com" {
		t.Errorf("主机应去重并排序，实际 %+v", llmStat.Hosts)
	}
	if _, bad := byKind["file.write"]; bad {
		t.Errorf("非出网留痕不能混进出网统计：%+v", rep.Stats)
	}
	if webStat, ok := byKind["web.fetch"]; !ok || webStat.Hosts[0] != "example.com" {
		t.Errorf("web.fetch 一类读数不对：%+v", rep.Stats)
	}
}

func TestEgressStatsNilGateSafe(t *testing.T) {
	var g *Gate
	rep := g.EgressStats()
	if rep.Scanned != 0 || len(rep.Stats) != 0 || rep.Cap != auditCap {
		t.Errorf("nil 门控应返回空读数，实际 %+v", rep)
	}
}
