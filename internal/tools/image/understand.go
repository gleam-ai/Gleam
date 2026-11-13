package image

import (
	"context"
	"encoding/base64"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"gleam/internal/llm"
	"gleam/internal/tools/toolutil"
	"gleam/pkg/types"
)

// ChatSource fetches the current main-model client at runtime so the tool
// follows hot-swapped settings and the task-pinned client inside a goal.
type ChatSource func(ctx context.Context) llm.Client

type Understand struct {
	Chat  ChatSource
	Roots RootsSource
}

func NewUnderstand(chat ChatSource, roots RootsSource) *Understand {
	return &Understand{Chat: chat, Roots: roots}
}

func (t *Understand) Name() string { return "image.understand" }

func (t *Understand) Description() string {
	return "读图：把工作区内的一张本地图片（png/jpeg/webp/gif）交给当前主模型做视觉理解，返回文字回答。" +
		"需要主模型支持视觉且使用 OpenAI 兼容 chat 协议；不支持时会明确报错。"
}

func (t *Understand) Permission() types.Permission { return types.PermissionUserApproved }

func (t *Understand) Schema() map[string]any {
	return toolutil.Schema("理解一张本地图片", []string{"path"}, map[string]any{
		"path":     toolutil.SchemaProp("图片路径（工作区内，png/jpeg/webp/gif）", "string"),
		"question": toolutil.SchemaProp("可选：想问图片的问题，默认描述图片内容", "string"),
	})
}

func (t *Understand) Execute(ctx context.Context, args map[string]any) (any, error) {
	path, err := toolutil.RequireStr(args, "path")
	if err != nil {
		return nil, err
	}
	abs, err := toolutil.ResolveInRoots(path, t.Roots(ctx))
	if err != nil {
		return nil, err
	}
	mime, err := mimeOfExt(filepath.Ext(abs))
	if err != nil {
		return nil, err
	}
	info, err := os.Stat(abs)
	if err != nil {
		return nil, fmt.Errorf("读取图片失败: %w", err)
	}
	if info.Size() > 8<<20 {
		return nil, fmt.Errorf("图片过大（%d 字节），请压缩到 8MB 以内", info.Size())
	}
	data, err := os.ReadFile(abs)
	if err != nil {
		return nil, fmt.Errorf("读取图片失败: %w", err)
	}
	client := t.Chat(ctx)
	if client == nil {
		return nil, fmt.Errorf("主模型未配置，无法读图")
	}
	question := strings.TrimSpace(toolutil.Str(args, "question"))
	if question == "" {
		question = "请详细描述这张图片的内容。"
	}
	req := llm.ChatRequest{
		Messages: []llm.Message{{
			Role:    llm.RoleUser,
			Content: question,
			Images:  []llm.ImagePart{{MIME: mime, Data: base64.StdEncoding.EncodeToString(data)}},
		}},
		Temperature: 0.2,
	}
	answer, err := client.Chat(ctx, req)
	if err != nil {
		return nil, fmt.Errorf("视觉理解失败: %w", err)
	}
	return map[string]any{
		"answer": strings.TrimSpace(answer),
		"path":   abs,
		"model":  client.Name(),
	}, nil
}

func mimeOfExt(ext string) (string, error) {
	switch strings.ToLower(ext) {
	case ".png":
		return "image/png", nil
	case ".jpg", ".jpeg":
		return "image/jpeg", nil
	case ".webp":
		return "image/webp", nil
	case ".gif":
		return "image/gif", nil
	default:
		return "", fmt.Errorf("不支持的图片格式 %q（仅 png/jpeg/webp/gif）", ext)
	}
}
