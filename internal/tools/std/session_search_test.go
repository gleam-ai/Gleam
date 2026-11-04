package std

import (
	"context"
	"testing"

	"gleam/internal/harness/conversation"
	"gleam/pkg/types"
)

type fakeSearcher struct {
	hits []conversation.SearchHit
	err  error
}

func (f *fakeSearcher) Search(query string, limit int) ([]conversation.SearchHit, error) {
	return f.hits, f.err
}

func TestSessionSearchTool_ReturnsHits(t *testing.T) {
	tool := NewSessionSearch(&fakeSearcher{hits: []conversation.SearchHit{
		{ID: "c1", Title: "部署脚本", Count: 3, Excerpts: []string{"标题：部署脚本"}},
	}})
	out, err := tool.Execute(context.Background(), map[string]any{"query": "部署", "limit": float64(5)})
	if err != nil {
		t.Fatal(err)
	}
	m, ok := out.(map[string]any)
	if !ok || m["count"] != 1 {
		t.Fatalf("out = %+v", out)
	}
	if m["query"] != "部署" {
		t.Errorf("query = %v", m["query"])
	}
}

func TestSessionSearchTool_EmptyResultIsEmptyOutcome(t *testing.T) {
	tool := NewSessionSearch(&fakeSearcher{})
	out, err := tool.Execute(context.Background(), map[string]any{"query": "不存在"})
	if err != nil {
		t.Fatal(err)
	}
	outcome, _ := tool.(types.OutcomeReporter).Outcome(nil, out)
	if outcome != types.OutcomeEmpty {
		t.Errorf("outcome = %q, want empty", outcome)
	}
}

func TestSessionSearchTool_MissingQuery(t *testing.T) {
	tool := NewSessionSearch(&fakeSearcher{})
	if _, err := tool.Execute(context.Background(), map[string]any{}); err == nil {
		t.Fatal("缺 query 应报错")
	}
}
