package std

import (
	"context"
	"fmt"
	"strings"

	"gleam/internal/tools/toolutil"
	"gleam/pkg/types"
)

// TodoItem is a single todo written by the model. Status only allows
// pending/in_progress/done.
type TodoItem struct {
	Text   string `json:"text"`
	Status string `json:"status"`
}

// TodoSink is implemented by the runtime (agent / webui): it receives and
// broadcasts todo snapshots. Every write replaces the whole list; there is no
// incremental merge. The model's "where am I now" is one integral state, and
// merging would leave stale entries on the UI.
type TodoSink interface {
	PublishTodos(ctx context.Context, items []TodoItem) error
}

type todoWriteTool struct {
	sink TodoSink
}

// NewTodoWrite registers the todo_write tool: it lets the model write down
// the current todo list for the frontend to render as progress. Unlike
// ProgressEvent, todos are structured and checkable.
func NewTodoWrite(sink TodoSink) types.Tool { return &todoWriteTool{sink} }

func (t *todoWriteTool) Name() string { return "todo_write" }

func (t *todoWriteTool) Description() string {
	return "把当前任务的待办清单写下来，让用户在 UI 里看到你的推进计划。每次调用都是整份替换；已完成的标 done，进行中的标 in_progress，未开始的标 pending。"
}

func (t *todoWriteTool) Permission() types.Permission { return types.PermissionReadOnly }

func (t *todoWriteTool) Schema() map[string]any {
	return toolutil.Schema("写入待办清单", []string{"items"}, map[string]any{
		"items": map[string]any{
			"type":        "array",
			"description": "待办条目列表，每项含 text 与 status",
			"items": map[string]any{
				"type": "object",
				"properties": map[string]any{
					"text":   toolutil.SchemaProp("待办内容（一句话）", "string"),
					"status": map[string]any{"type": "string", "enum": []string{"pending", "in_progress", "done"}, "description": "状态"},
				},
				"required": []string{"text", "status"},
			},
		},
	})
}

func (t *todoWriteTool) Execute(ctx context.Context, args map[string]any) (any, error) {
	if t.sink == nil {
		return nil, fmt.Errorf("当前运行环境不支持待办")
	}
	raw, ok := args["items"].([]any)
	if !ok {
		return nil, fmt.Errorf("items 必须是数组")
	}
	if len(raw) == 0 {
		return nil, fmt.Errorf("items 不能为空；若要清空请显式传 done 状态")
	}
	// Defensive: the model occasionally emits an overly long list, which
	// overflows the frontend LiveRail rendering.
	if len(raw) > 20 {
		raw = raw[:20]
	}

	items := make([]TodoItem, 0, len(raw))
	for i, r := range raw {
		m, ok := r.(map[string]any)
		if !ok {
			return nil, fmt.Errorf("items[%d] 不是对象", i)
		}
		text := strings.TrimSpace(toolutil.Str(m, "text"))
		if text == "" {
			return nil, fmt.Errorf("items[%d].text 不能为空", i)
		}
		status := strings.TrimSpace(toolutil.Str(m, "status"))
		switch status {
		case "pending", "in_progress", "done":
		default:
			return nil, fmt.Errorf("items[%d].status 必须是 pending/in_progress/done，得到 %q", i, status)
		}
		items = append(items, TodoItem{Text: text, Status: status})
	}
	if err := t.sink.PublishTodos(ctx, items); err != nil {
		return nil, err
	}
	done := 0
	for _, it := range items {
		if it.Status == "done" {
			done++
		}
	}
	return map[string]any{"count": len(items), "done": done}, nil
}
