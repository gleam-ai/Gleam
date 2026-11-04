package std

import (
	"context"
	"fmt"
	"strings"
	"time"

	"gleam/internal/tools/toolutil"
	"gleam/pkg/types"
)

// UserQuestion is a single question the model raises to the user.
type UserQuestion struct {
	Question string   `json:"question"`
	Choices  []string `json:"choices,omitempty"`
}

// UserAnswer is the user's response to a question. At least one of FreeText
// and Choice must be non-empty.
type UserAnswer struct {
	Choice   string `json:"choice,omitempty"`
	FreeText string `json:"free_text,omitempty"`
}

// QuestionAsker is implemented by the runtime (Web UI / CLI): it hands the
// question to the user and waits for the answer. Implementations must honor
// ctx cancellation and return an error on timeout.
type QuestionAsker interface {
	AskUser(ctx context.Context, q UserQuestion) (UserAnswer, error)
}

type askUserTool struct {
	asker QuestionAsker
}

// NewAskUser registers the ask-user tool. The model calls it when facing
// ambiguity or needing the user to decide, instead of guessing and moving on.
func NewAskUser(asker QuestionAsker) types.Tool { return &askUserTool{asker} }

func (t *askUserTool) Name() string { return "ask_user" }

func (t *askUserTool) Description() string {
	return "当你无法确定用户意图、需要用户在多个选项中拍板、或缺少关键信息时，调用此工具向用户提问。比猜测更可靠。"
}

func (t *askUserTool) Permission() types.Permission { return types.PermissionReadOnly }

func (t *askUserTool) Schema() map[string]any {
	return toolutil.Schema("向用户提问", []string{"question"}, map[string]any{
		"question": toolutil.SchemaProp("要问用户的问题（一句话、清晰具体）", "string"),
		"choices":  toolutil.SchemaProp("可选：候选答案列表，用户可从中选择或直接输入", "array"),
	})
}

func (t *askUserTool) Execute(ctx context.Context, args map[string]any) (any, error) {
	if t.asker == nil {
		return nil, fmt.Errorf("当前运行环境不支持向用户提问")
	}
	q, err := toolutil.RequireStr(args, "question")
	if err != nil {
		return nil, err
	}
	if strings.TrimSpace(q) == "" {
		return nil, fmt.Errorf("question 不能为空")
	}
	choices := toolutil.StrSlice(args, "choices")
	// Defensive: too many choices make the frontend card hard to lay out, and
	// usually mean the model is dumping options instead of judging.
	if len(choices) > 6 {
		choices = choices[:6]
	}

	qctx, cancel := context.WithTimeout(ctx, 5*time.Minute)
	defer cancel()
	ans, err := t.asker.AskUser(qctx, UserQuestion{
		Question: q,
		Choices:  choices,
	})
	if err != nil {
		return nil, fmt.Errorf("提问未完成: %w", err)
	}
	out := map[string]any{}
	if ans.Choice != "" {
		out["choice"] = ans.Choice
	}
	if ans.FreeText != "" {
		out["free_text"] = ans.FreeText
	}
	if len(out) == 0 {
		out["free_text"] = "(用户未作答)"
	}
	return out, nil
}
