package prompt

import (
	"context"
	"fmt"
	"strings"

	"gleam/internal/llm"
	"gleam/internal/tools/toolutil"
	"gleam/pkg/types"
)

type ClientProvider func() llm.Client

type GenerateTool struct {
	provider ClientProvider
}

func NewGenerate(p ClientProvider) *GenerateTool { return &GenerateTool{provider: p} }

func (t *GenerateTool) Name() string { return "prompt.run" }

func (t *GenerateTool) Description() string {
	return "把一段提示词交给模型执行，返回它的回答。外部技能（提示词包）靠它落地：技能正文就是提示词，这一步负责跑。"
}

func (t *GenerateTool) Permission() types.Permission { return types.PermissionUserApproved }

func (t *GenerateTool) Schema() map[string]any {
	return toolutil.Schema("调用模型执行一段提示词", []string{"prompt"}, map[string]any{
		"prompt": toolutil.SchemaProp("要执行的提示词（技能正文照原样放这里）", "string"),
		"system": toolutil.SchemaProp("可选的系统提示词", "string"),
		"input":  toolutil.SchemaProp("可选：本次要处理的素材，会附在提示词之后", "string"),
	})
}

func (t *GenerateTool) Execute(ctx context.Context, args map[string]any) (any, error) {
	raw, err := toolutil.RequireStr(args, "prompt")
	if err != nil {
		return nil, err
	}
	prompt := strings.TrimSpace(raw)
	if prompt == "" {
		return nil, fmt.Errorf("提示词为空")
	}
	if t.provider == nil {
		return nil, fmt.Errorf("提示词步骤没接上模型客户端")
	}
	client := t.provider()
	if client == nil {
		return nil, fmt.Errorf("当前没有可用的模型：请先在「设置 → 模型」里接入一个")
	}
	if input := strings.TrimSpace(toolutil.Str(args, "input")); input != "" {
		prompt = prompt + "\n\n---\n\n" + input
	}
	text, err := client.Chat(ctx, llm.ChatRequest{
		System: strings.TrimSpace(toolutil.Str(args, "system")),
		Messages: []llm.Message{
			{Role: "user", Content: prompt},
		},
	})
	if err != nil {
		return nil, fmt.Errorf("调用模型失败：%w", err)
	}
	return map[string]any{"text": text, "model": client.Name()}, nil
}
