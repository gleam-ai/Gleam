package eval

import (
	"context"
	"testing"

	"gleam/internal/agent"
)

// TestFullDepthArchivesTaskAnswer proves the eval runner records the goal's
// answer (summary + task id) on the case and archives the full result, so a
// reviewer can check correctness with `gleam replay` instead of re-running
// `gleam goal` by hand.
func TestFullDepthArchivesTaskAnswer(t *testing.T) {
	r := fullDepthRunner(t, &usageLLM{}, 1)
	rep := r.Run(context.Background(), []Case{{ID: "a1", Goal: "打个招呼"}})

	c := rep.Cases[0]
	if c.TaskID == "" {
		t.Fatal("用例结果没有 task_id，无法回放复核")
	}
	if c.Summary == "" {
		t.Fatal("用例结果没有 summary，答案内容未被记录")
	}

	g, err := agent.ReadTaskResult(r.Agent.Config().DataDir, c.TaskID)
	if err != nil {
		t.Fatalf("读取归档失败：%v", err)
	}
	if g == nil {
		t.Fatal("任务未归档：ReadTaskResult 返回 nil")
	}
	if g.Summary != c.Summary {
		t.Errorf("归档 summary %q 与用例 summary %q 不一致", g.Summary, c.Summary)
	}
}
