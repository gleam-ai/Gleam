// Package image provides the text-to-image tool: it calls an OpenAI-compatible
// images/generations endpoint (GLM cogview, OpenAI gpt-image-1/dall-e-3, and
// assorted compatible gateways), writes the generated image into the
// workspace, and returns the path.
//
// Both response shapes are accepted: b64_json (decoded directly) and url
// (downloaded). Image bytes never enter the model context; only the path and
// metadata come back, matching Gleam's "wire protocol carries text only"
// policy.
package image

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"gleam/internal/atomicfile"
	"gleam/internal/tools/toolutil"
	"gleam/internal/tools/web"
	"gleam/pkg/types"
)

// KeySource fetches the image API connection details at runtime: baseURL
// (e.g. GLM's https://open.bigmodel.cn/api/paas/v4), the API key, and the
// image model name (cfg.LLM.ImageModel, e.g. cogview-4-250304).
type KeySource func(ctx context.Context) (baseURL, apiKey, model string, err error)

// RootsSource fetches the workspace roots fresh on every call (workspaces
// hot-swap; a fixed root would keep pointing at a stale directory).
type RootsSource func(ctx context.Context) []string

type Generate struct {
	Source  KeySource
	Roots   RootsSource
	Timeout time.Duration
	// httpPost / httpGet are test seams; nil uses real HTTP.
	httpPost func(ctx context.Context, url, key string, body []byte) (int, []byte, error)
	httpGet  func(ctx context.Context, url string) ([]byte, error)
	// OnEgress accounts for outbound traffic (wired to safety.Gate.RecordEgress).
	OnEgress func(host string, nbytes int)
}

func NewGenerate(source KeySource, roots RootsSource) *Generate {
	return &Generate{Source: source, Roots: roots, Timeout: 120 * time.Second}
}

func (t *Generate) Name() string { return "image.generate" }

func (t *Generate) Description() string {
	return "文生图：把提示词交给厂商图像模型（OpenAI 兼容 images API；GLM 用 cogview 系列，" +
		"需配置 llm.image_model），图片保存到工作区 images/ 目录，返回文件路径。" +
		"当前接入的主模型若不支持图像 API 会明确报错。"
}

func (t *Generate) Permission() types.Permission { return types.PermissionUserApproved }

func (t *Generate) Schema() map[string]any {
	return toolutil.Schema("生成图片", []string{"prompt"}, map[string]any{
		"prompt":    toolutil.SchemaProp("画面描述：主体、风格、构图、光影，越具体越好", "string"),
		"file_name": toolutil.SchemaProp("可选：保存文件名（不含扩展名），默认 image-<时间戳>", "string"),
	})
}

type imagesRequest struct {
	Model  string `json:"model"`
	Prompt string `json:"prompt"`
	N      int    `json:"n"`
}

type imagesResponse struct {
	Data []struct {
		B64JSON string `json:"b64_json"`
		URL     string `json:"url"`
	} `json:"data"`
	Error *struct {
		Message string `json:"message"`
	} `json:"error,omitempty"`
}

func (t *Generate) Execute(ctx context.Context, args map[string]any) (any, error) {
	prompt := strings.TrimSpace(toolutil.Str(args, "prompt"))
	if prompt == "" {
		return nil, fmt.Errorf("缺少必填参数 %q", "prompt")
	}
	if t.Source == nil {
		return nil, fmt.Errorf("图像生成未接入运行时")
	}
	baseURL, apiKey, model, err := t.Source(ctx)
	if err != nil {
		return nil, err
	}
	if strings.TrimSpace(model) == "" {
		return nil, fmt.Errorf("未配置 llm.image_model（GLM 填 cogview-4-250304，OpenAI 填 gpt-image-1；当前主模型 %s 不代表支持图像）", baseURL)
	}
	if strings.TrimSpace(apiKey) == "" {
		return nil, fmt.Errorf("图像 API 未配置密钥（与主模型同一 base_url 的密钥未设置）")
	}

	endpoint := strings.TrimRight(baseURL, "/") + "/images/generations"
	reqBody, err := json.Marshal(imagesRequest{Model: model, Prompt: prompt, N: 1})
	if err != nil {
		return nil, err
	}
	timeout := t.Timeout
	if timeout <= 0 {
		timeout = 120 * time.Second
	}
	pctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	status, body, err := t.post(pctx, endpoint, apiKey, reqBody)
	if err != nil {
		return nil, fmt.Errorf("图像 API 请求失败: %w", err)
	}
	if status < 200 || status >= 300 {
		return nil, fmt.Errorf("图像 API 返回 %d: %s", status, types.Shorten(string(body), 300))
	}
	var resp imagesResponse
	if err := json.Unmarshal(body, &resp); err != nil {
		return nil, fmt.Errorf("图像 API 响应不是 JSON: %w", err)
	}
	if resp.Error != nil && resp.Error.Message != "" {
		return nil, fmt.Errorf("图像 API 错误: %s", resp.Error.Message)
	}
	if len(resp.Data) == 0 {
		return nil, fmt.Errorf("图像 API 返回空结果")
	}

	var data []byte
	switch {
	case resp.Data[0].B64JSON != "":
		if data, err = base64.StdEncoding.DecodeString(resp.Data[0].B64JSON); err != nil {
			return nil, fmt.Errorf("b64_json 解码失败: %w", err)
		}
	case resp.Data[0].URL != "":
		if data, err = t.get(pctx, resp.Data[0].URL); err != nil {
			return nil, fmt.Errorf("下载生成图片失败: %w", err)
		}
	default:
		return nil, fmt.Errorf("图像 API 响应既无 b64_json 也无 url")
	}

	ext := sniffExt(data)
	if ext == "" {
		return nil, fmt.Errorf("响应字节不是可识别的图片格式（PNG/JPEG/WebP）")
	}
	path, err := t.save(ctx, toolutil.Str(args, "file_name"), ext, data)
	if err != nil {
		return nil, err
	}
	return map[string]any{
		"path": path, "bytes": len(data), "model": model,
		"prompt": prompt,
	}, nil
}

func (t *Generate) post(ctx context.Context, url, key string, body []byte) (int, []byte, error) {
	if t.httpPost != nil {
		return t.httpPost(ctx, url, key, body)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return 0, nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+key)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return 0, nil, err
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, 64<<20))
	if err == nil && t.OnEgress != nil {
		if host := req.URL.Host; host != "" {
			t.OnEgress(host, len(raw))
		}
	}
	return resp.StatusCode, raw, err
}

func (t *Generate) get(ctx context.Context, url string) ([]byte, error) {
	if t.httpGet != nil {
		return t.httpGet(ctx, url)
	}
	// The URL is chosen by the remote image API, not by the user. Guard it
	// with the same private-network policy as web.fetch before connecting.
	if err := web.GuardURL(ctx, url); err != nil {
		return nil, fmt.Errorf("拒绝下载生成图片：%w", err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	resp, err := web.GuardedClient(30 * time.Second).Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("下载返回 %d", resp.StatusCode)
	}
	raw, err := io.ReadAll(io.LimitReader(resp.Body, 64<<20))
	if err == nil && t.OnEgress != nil {
		if host := req.URL.Host; host != "" {
			t.OnEgress(host, len(raw))
		}
	}
	return raw, err
}

// sniffExt identifies the format by file magic; generated-result URLs often
// carry no extension, so inspecting the bytes is the most reliable signal.
func sniffExt(b []byte) string {
	switch {
	case len(b) >= 8 && bytes.HasPrefix(b, []byte("\x89PNG\r\n\x1a\n")):
		return "png"
	case len(b) >= 3 && bytes.HasPrefix(b, []byte("\xff\xd8\xff")):
		return "jpg"
	case len(b) >= 12 && bytes.HasPrefix(b, []byte("RIFF")) && bytes.Equal(b[8:12], []byte("WEBP")):
		return "webp"
	default:
		return ""
	}
}

func (t *Generate) save(ctx context.Context, fileName, ext string, data []byte) (string, error) {
	var roots []string
	if t.Roots != nil {
		roots = t.Roots(ctx)
	}
	if len(roots) == 0 || strings.TrimSpace(roots[0]) == "" {
		return "", fmt.Errorf("没有可用工作区，图片无处保存")
	}
	name := sanitizeName(fileName)
	if name == "" {
		name = fmt.Sprintf("image-%d", time.Now().Unix())
	}
	dir := filepath.Join(roots[0], "images")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}
	path := filepath.Join(dir, name+"."+ext)
	// ResolveInRoots guarantees ../.. cannot steer the save path outside the
	// workspace.
	abs, err := toolutil.ResolveInRoots(path, toolutil.RootsFrom(ctx, roots))
	if err != nil {
		return "", err
	}
	if err := atomicfile.Write(abs, data, 0o644); err != nil {
		return "", err
	}
	return abs, nil
}

func sanitizeName(s string) string {
	s = strings.TrimSpace(s)
	var b strings.Builder
	for _, r := range s {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9',
			r == '-', r == '_':
			b.WriteRune(r)
		default:
			b.WriteRune('-')
		}
	}
	out := strings.Trim(b.String(), "-.")
	if len(out) > 64 {
		out = out[:64]
	}
	return out
}
