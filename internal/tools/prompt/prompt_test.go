package prompt

import (
	"context"
	"strings"
	"testing"

	"gleam/internal/llm"
)

type fakeClient struct {
	last llm.ChatRequest
}

func (f *fakeClient) Chat(_ context.Context, req llm.ChatRequest) (string, error) {
	f.last = req
	return "模型回答", nil
}
func (f *fakeClient) ChatStream(ctx context.Context, req llm.ChatRequest, _ func(string)) (string, error) {
	return f.Chat(ctx, req)
}
func (f *fakeClient) Name() string { return "fake-model" }

func TestPromptRunSendsPromptAndInput(t *testing.T) {
	f := &fakeClient{}
	tool := NewGenerate(func() llm.Client { return f })

	out, err := tool.Execute(context.Background(), map[string]any{
		"prompt": "按这个技能做：把素材改写成三点",
		"system": "你是编辑",
		"input":  "素材原文",
	})
	if err != nil {
		t.Fatal(err)
	}
	m, _ := out.(map[string]any)
	if m["text"] != "模型回答" || m["model"] != "fake-model" {
		t.Errorf("返回值不对：%v", m)
	}
	if f.last.System != "你是编辑" {
		t.Errorf("系统提示词没传下去：%q", f.last.System)
	}
	if len(f.last.Messages) != 1 || !strings.Contains(f.last.Messages[0].Content, "把素材改写成三点") {
		t.Fatalf("提示词正文没传下去：%+v", f.last.Messages)
	}
	if !strings.Contains(f.last.Messages[0].Content, "素材原文") {
		t.Error("素材应附在提示词之后")
	}
}

func TestPromptRunWithoutModelSaysHowToFix(t *testing.T) {
	tool := NewGenerate(func() llm.Client { return nil })
	_, err := tool.Execute(context.Background(), map[string]any{"prompt": "做点什么"})
	if err == nil {
		t.Fatal("没有模型时应当报错")
	}
	if !strings.Contains(err.Error(), "设置") {
		t.Errorf("错误里要给出下一步（去设置接个模型）：%v", err)
	}
}

func TestPromptRunRejectsEmptyPrompt(t *testing.T) {
	tool := NewGenerate(func() llm.Client { return &fakeClient{} })
	if _, err := tool.Execute(context.Background(), map[string]any{"prompt": "   "}); err == nil {
		t.Fatal("空提示词应被拒")
	}
	if _, err := tool.Execute(context.Background(), map[string]any{}); err == nil {
		t.Fatal("缺 prompt 参数应被拒")
	}
}

func TestPromptRunPermissionIsReadOnly(t *testing.T) {
	tool := NewGenerate(func() llm.Client { return &fakeClient{} })
	if got := tool.Permission().String(); got != "readonly" {
		t.Errorf("提示词步骤应是只读放行，得到 %q", got)
	}
	if tool.Name() != "prompt.run" {
		t.Errorf("工具名 = %q", tool.Name())
	}
}
