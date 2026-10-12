package llm

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"sync"
)

type Scripted struct {
	Kind  string   `json:"kind"`
	Texts []string `json:"texts"`
}

type MockClient struct {
	mu         sync.Mutex
	scripts    map[string][]string
	handler    func(req ChatRequest) string
	Calls      []ChatRequest
	Model      string
	failNext   error
	failRemain int
}

func NewMock() *MockClient {
	return &MockClient{scripts: map[string][]string{}, Model: "mock"}
}

func (m *MockClient) Enqueue(kind string, texts ...string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.scripts[kind] = append(m.scripts[kind], texts...)
}

func (m *MockClient) SetHandler(h func(req ChatRequest) string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.handler = h
}

func (m *MockClient) FailNext(err error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.failNext = err
	m.failRemain = 1
}

func (m *MockClient) FailNextN(n int, err error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.failNext = err
	m.failRemain = n
}

func (m *MockClient) Name() string { return m.Model }

func (m *MockClient) next(req ChatRequest) (string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.Calls = append(m.Calls, req)
	if m.failRemain > 0 && m.failNext != nil {
		m.failRemain--
		return "", m.failNext
	}
	kind := KindOf(req.System)
	if q := m.scripts[kind]; len(q) > 0 {
		text := q[0]
		m.scripts[kind] = q[1:]
		return text, nil
	}
	if m.handler != nil {
		return m.handler(req), nil
	}
	return m.defaultReply(kind, req), nil
}

func (m *MockClient) Chat(ctx context.Context, req ChatRequest) (string, error) {
	text, err := m.next(req)
	if err != nil {
		return "", err
	}
	ReportUsage(req, text, Usage{})
	return text, nil
}

func (m *MockClient) ChatStream(ctx context.Context, req ChatRequest, onDelta func(string)) (string, error) {
	text, err := m.next(req)
	if err != nil {
		return "", err
	}
	ReportUsage(req, text, Usage{})
	runes := []rune(text)
	for i := 0; i < len(runes); i += 8 {
		end := i + 8
		if end > len(runes) {
			end = len(runes)
		}
		if onDelta != nil {
			onDelta(string(runes[i:end]))
		}
	}
	return text, nil
}

func (m *MockClient) defaultReply(kind string, req ChatRequest) string {
	switch kind {
	case "plan":
		goal := planGoal(req)
		plan := map[string]any{
			"steps": []map[string]any{{
				"id":          "s1",
				"description": "直接回复用户",
				"tool":        "reply",
				"args":        map[string]any{"text": "（Mock 模式）收到目标：" + goal},
			}},
			"estimated_time": "short",
		}
		b, _ := json.Marshal(plan)
		return string(b)
	case "reflect":
		return `{"score":85,"verdict":"done","reason":"Mock 默认评估：执行无异常"}`
	case "compress":
		text := messagesText(req.Messages)
		runes := []rune(text)
		if len(runes) > 160 {
			runes = runes[:160]
		}
		return "（Mock 摘要）" + strings.TrimSpace(string(runes))
	case "geo":
		return `{"score":75,"summary":"Mock GEO 分析：结构清晰，建议补充数据支撑","strengths":["段落组织合理","核心概念明确"],"weaknesses":["缺少具体案例","关键词密度偏低"],"actionables":[{"category":"引用性","description":"补充 1-2 个具体数据或案例","priority":"high"},{"category":"关键词","description":"在小标题中自然融入核心术语","priority":"medium"}]}`
	case "chat_mode":
		text := lastUserText(req)
		runes := []rune(text)
		if len(runes) > 120 {
			runes = append([]rune(string(runes[:120])), '…')
		}
		return "（Mock 对话）" + string(runes)
	default:
		return "（Mock 响应）"
	}
}

func planGoal(req ChatRequest) string {
	text := lastUserText(req)
	if text == "" {
		text = strings.TrimSpace(req.System)
	}
	if i := strings.LastIndex(text, "用户目标："); i >= 0 {
		rest := strings.TrimSpace(text[i+len("用户目标："):])
		if j := strings.IndexByte(rest, '\n'); j >= 0 {
			rest = rest[:j]
		}
		text = rest
	}
	if text == "" {
		return "（mock 未从请求里取到目标文本）"
	}
	runes := []rune(text)
	if len(runes) > 80 {
		runes = append(runes[:80], '…')
	}
	return string(runes)
}

func lastUserText(req ChatRequest) string {
	for i := len(req.Messages) - 1; i >= 0; i-- {
		if req.Messages[i].Role == RoleUser {
			return strings.TrimSpace(req.Messages[i].Content)
		}
	}
	return ""
}

func messagesText(msgs []Message) string {
	var b strings.Builder
	for _, m := range msgs {
		b.WriteString(m.Content)
		b.WriteString("\n")
	}
	return b.String()
}

func LoadScripts(path string) ([]Scripted, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("mock: 读取脚本失败: %w", err)
	}
	var scripts []Scripted
	if err := json.Unmarshal(data, &scripts); err != nil {
		return nil, fmt.Errorf("mock: 解析脚本失败: %w", err)
	}
	return scripts, nil
}

func (m *MockClient) Apply(scripts []Scripted) {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, s := range scripts {
		m.scripts[s.Kind] = append(m.scripts[s.Kind], s.Texts...)
	}
}
