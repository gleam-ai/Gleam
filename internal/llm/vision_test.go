package llm

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
)

func TestGLMClient_VisionContentParts(t *testing.T) {
	c := NewGLM("https://example.com", "k", "glm-4v", 0.1, 100, 10)
	req := ChatRequest{
		System: "sys",
		Messages: []Message{
			{Role: RoleUser, Content: " plain"},
			{Role: RoleUser, Content: "图里有什么", Images: []ImagePart{
				{MIME: "image/png", Data: "QUJD"},
				{URL: "https://example.com/a.png"},
			}},
		},
	}
	msgs := c.buildMessages(req)
	if len(msgs) != 3 {
		t.Fatalf("消息数 = %d", len(msgs))
	}
	if s, ok := msgs[1].Content.(string); !ok || s != " plain" {
		t.Errorf("无图消息应保持纯字符串: %#v", msgs[1].Content)
	}
	parts, ok := msgs[2].Content.([]map[string]any)
	if !ok || len(parts) != 3 {
		t.Fatalf("有图消息应为 3 个 content part: %#v", msgs[2].Content)
	}
	if parts[0]["type"] != "text" || parts[0]["text"] != "图里有什么" {
		t.Errorf("文本 part = %#v", parts[0])
	}
	url := parts[1]["image_url"].(map[string]any)["url"].(string)
	if url != "data:image/png;base64,QUJD" {
		t.Errorf("内联图 data URI = %q", url)
	}
	url = parts[2]["image_url"].(map[string]any)["url"].(string)
	if url != "https://example.com/a.png" {
		t.Errorf("远端图 URL = %q", url)
	}
	body, err := json.Marshal(c.payload(req, false))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(body), `"image_url"`) {
		t.Error("请求体应含 image_url part")
	}
}

func TestGLMClient_TextOnlyPayloadUnchanged(t *testing.T) {
	c := NewGLM("https://example.com", "k", "glm-4", 0.1, 100, 10)
	req := ChatRequest{Messages: []Message{{Role: RoleUser, Content: "你好"}}}
	body, err := json.Marshal(c.payload(req, false))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(body), `"content":"你好"`) {
		t.Errorf("纯文本请求体应保持字符串 content: %s", body)
	}
}

func TestProtocolsRejectImagesExplicitly(t *testing.T) {
	req := ChatRequest{Messages: []Message{{Role: RoleUser, Content: "看图", Images: []ImagePart{{MIME: "image/png", Data: "QUJD"}}}}}
	anthropic := NewAnthropic("https://example.com", "k", "claude", 0.1, 100, 10)
	responses := NewResponses("https://example.com", "k", "gpt", 0.1, 100, 10)
	for _, c := range []Client{anthropic, responses} {
		if _, err := c.Chat(context.Background(), req); !errors.Is(err, ErrImagesUnsupported) {
			t.Errorf("%T Chat 应显式拒绝图片: %v", c, err)
		}
		if _, err := c.ChatStream(context.Background(), req, nil); !errors.Is(err, ErrImagesUnsupported) {
			t.Errorf("%T ChatStream 应显式拒绝图片: %v", c, err)
		}
	}
}
