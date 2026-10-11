// providers.go 内置各大模型厂商官方接入预设与协议工厂。
// 套餐类型（token 按量 / coding 编程包月 / agent 智能体套餐）映射到各厂对应的官方入口；
// 预设仅是默认值，用户可在设置页修改任意字段（以厂商最新文档为准）。
package llm

import (
	"net/url"
	"strings"
	"time"
)

// 协议常量（自定义接入支持的三种线协议）。
const (
	ProtocolOpenAIChat      = "openai_chat"      // OpenAI Chat Completions 兼容（含 GLM/DeepSeek/Kimi/Qwen 等）
	ProtocolOpenAIResponses = "openai_responses" // OpenAI Responses API
	ProtocolAnthropic       = "anthropic"        // Anthropic Messages API
)

// 套餐类型。
const (
	PlanToken  = "token"  // 按量计费（通用入口）
	PlanCoding = "coding" // 编程套餐（包月，独立高优先级入口）
	PlanAgent  = "agent"  // 智能体套餐
)

// PlanPreset 厂商某类套餐的官方入口。
type PlanPreset struct {
	Kind     string `json:"kind"`     // token | coding | agent
	Label    string `json:"label"`    // 显示名（如 "编程套餐 Coding Plan"）
	BaseURL  string `json:"base_url"` // 官方 API 入口
	Protocol string `json:"protocol"` // openai_chat | openai_responses | anthropic
	Model    string `json:"model"`    // 该套餐的默认模型（可改）
}

// ProviderPreset 厂商预设。
type ProviderPreset struct {
	ID          string       `json:"id"`
	Name        string       `json:"name"`
	Plans       []PlanPreset `json:"plans"`
	Recommended bool         `json:"recommended,omitempty"` // 推荐厂商（TokenDance/DeepSeek）
	// Models 该厂商的内置模型 ID 清单。**定位是离线兜底，不是权威**：联网拉到的永远优先。
	// 按厂商存而不是按套餐存——同一家的模型跨套餐共用，按套餐存会重复三份。
	// 只放"能站得住"的几条（文档原文点名 + 该家自己的默认模型），不追求抄全：
	// 抄全必然过期，聚合商（OpenRouter/TokenDance）更是抄不进去。
	Models []string `json:"models"`
}

// Providers 内置厂商目录（2026-09 经官方文档核实；入口或套餐有变动时可在设置页直接改）。
// 依据：火山方舟 docs.volcengine.com/docs/82379/1925114（Coding Plan）、智谱 docs.bigmodel.cn/cn/coding-plan、
// 阿里云 help.aliyun.com/zh/model-studio/coding-plan、MiniMax platform.minimax.io、Kimi platform.kimi.com。
var Providers = []ProviderPreset{
	{
		ID: "tokendance", Name: "TokenDance 词元跳动（聚合）", Recommended: true,
		Plans: []PlanPreset{
			{Kind: PlanToken, Label: "按量 Token（OpenAI 兼容）", BaseURL: "https://tokendance.space/gateway/v1", Protocol: ProtocolOpenAIChat},
			{Kind: PlanAgent, Label: "Anthropic 兼容入口", BaseURL: "https://tokendance.space/gateway/v1", Protocol: ProtocolAnthropic},
		},
	},
	{
		ID: "deepseek", Name: "DeepSeek 深度求索", Recommended: true,
		Plans: []PlanPreset{
			{Kind: PlanToken, Label: "按量 Token（快速版，OpenAI 兼容）", BaseURL: "https://api.deepseek.com/v1", Protocol: ProtocolOpenAIChat},
			{Kind: PlanCoding, Label: "编程模式（旗舰版，复杂任务）", BaseURL: "https://api.deepseek.com/v1", Protocol: ProtocolOpenAIChat},
		},
	},
	{
		ID: "zhipu", Name: "智谱 GLM",
		Plans: []PlanPreset{
			{Kind: PlanToken, Label: "按量 Token（OpenAI 兼容）", BaseURL: "https://open.bigmodel.cn/api/paas/v4", Protocol: ProtocolOpenAIChat},
			{Kind: PlanCoding, Label: "GLM Coding Plan 编程套餐（OpenAI 兼容）", BaseURL: "https://open.bigmodel.cn/api/coding/paas/v4", Protocol: ProtocolOpenAIChat},
			{Kind: PlanAgent, Label: "Anthropic 兼容入口（Claude Code 同款）", BaseURL: "https://open.bigmodel.cn/api/anthropic", Protocol: ProtocolAnthropic},
		},
	},
	{
		ID: "moonshot", Name: "月之暗面 Kimi",
		Plans: []PlanPreset{
			{Kind: PlanToken, Label: "按量 Token（OpenAI 兼容）", BaseURL: "https://api.moonshot.cn/v1", Protocol: ProtocolOpenAIChat},
			{Kind: PlanCoding, Label: "Kimi Coding（Anthropic 兼容；托管版经 Kimi Code 登录自动配置）", BaseURL: "https://api.moonshot.cn/anthropic", Protocol: ProtocolAnthropic},
		},
	},
	{
		ID: "qwen", Name: "阿里云百炼（通义 Qwen）",
		Plans: []PlanPreset{
			{Kind: PlanToken, Label: "按量 Token（OpenAI 兼容）", BaseURL: "https://dashscope.aliyuncs.com/compatible-mode/v1", Protocol: ProtocolOpenAIChat},
			{Kind: PlanCoding, Label: "百炼 Coding Plan（Anthropic 兼容；Key 为 sk-sp- 开头，与按量不互通）", BaseURL: "https://coding.dashscope.aliyuncs.com/anthropic", Protocol: ProtocolAnthropic},
			{Kind: PlanAgent, Label: "百炼 Coding Plan（OpenAI 兼容；一个 Key 聚合千问/GLM/Kimi/MiniMax）", BaseURL: "https://coding.dashscope.aliyuncs.com/v1", Protocol: ProtocolOpenAIChat},
		},
	},
	{
		ID: "volc", Name: "火山方舟（豆包）",
		Plans: []PlanPreset{
			{Kind: PlanToken, Label: "按量 Token（OpenAI 兼容）", BaseURL: "https://ark.cn-beijing.volces.com/api/v3", Protocol: ProtocolOpenAIChat},
			{Kind: PlanCoding, Label: "方舟 Coding Plan（Lite/Pro，Anthropic 兼容，适配 Claude Code）", BaseURL: "https://ark.cn-beijing.volces.com/api/coding", Protocol: ProtocolAnthropic},
		},
	},
	{
		ID: "minimax", Name: "MiniMax",
		Plans: []PlanPreset{
			{Kind: PlanToken, Label: "按量 Token（OpenAI 兼容，国内站）", BaseURL: "https://api.minimaxi.com/v1", Protocol: ProtocolOpenAIChat},
			{Kind: PlanCoding, Label: "MiniMax Coding Plan（Anthropic 兼容，国内站；国际站为 api.minimax.io）", BaseURL: "https://api.minimaxi.com/anthropic", Protocol: ProtocolAnthropic},
		},
	},
	{
		ID: "openai", Name: "OpenAI",
		Plans: []PlanPreset{
			{Kind: PlanToken, Label: "按量 Token（Chat Completions）", BaseURL: "https://api.openai.com/v1", Protocol: ProtocolOpenAIChat},
			{Kind: PlanAgent, Label: "Responses API", BaseURL: "https://api.openai.com/v1", Protocol: ProtocolOpenAIResponses},
		},
	},
	{
		ID: "anthropic", Name: "Anthropic",
		Plans: []PlanPreset{
			{Kind: PlanToken, Label: "按量 Token（Messages API）", BaseURL: "https://api.anthropic.com/v1", Protocol: ProtocolAnthropic},
		},
	},
	{
		ID: "openrouter", Name: "OpenRouter（聚合）",
		Plans: []PlanPreset{
			{Kind: PlanToken, Label: "按量 Token", BaseURL: "https://openrouter.ai/api/v1", Protocol: ProtocolOpenAIChat},
		},
		// 聚合商的目录**故意只列能站得住的几条**：官方文档只点名了下面两个 ID，
		// 而 openrouter/auto 是它自己的路由入口（长期存在，是这家最稳的一个）。
		// 完整清单只存在于 GET /api/v1/models —— 那是"拉取"要干的事，不是这里要抄一遍的东西。
		// 出处：https://openrouter.ai/docs/quickstart（2026-10-11 抓取）
		Models: []string{
			"openrouter/auto",                 // 路由入口：由 OpenRouter 自己挑模型
			"~openai/gpt-sol-latest",          // 文档原文
			"~anthropic/claude-sonnet-latest", // 文档原文
		},
	},
	// ---- 以下 5 家为 2026-10-11 依官方文档新增 ----
	//
	// 收录规则：**只收能走文本对话的模型**。向量 / 重排 / OCR / 图像 / 视频 / TTS /
	// 纯语音一律不收——名单里混进一个不能对话的模型，比少列几个更坏（用户选中它，
	// 换来的是一次必然失败的校验）。每家上面写明出处，抄的是文档原文，不做推断。
	{
		ID: "gemini", Name: "Google Gemini",
		Plans: []PlanPreset{
			{Kind: PlanToken, Label: "按量 Token（OpenAI 兼容）", BaseURL: "https://generativelanguage.googleapis.com/v1beta/openai/", Protocol: ProtocolOpenAIChat},
		},
		// 出处：https://ai.google.dev/gemini-api/docs/openai（2026-10-11 抓取）
		Models: []string{
			"gemini-3.8-flash",
			"gemini-3.1-pro-preview",
			"gemini-3.5-flash-lite",
		},
	},
	{
		ID: "stepfun", Name: "阶跃星辰 StepFun",
		Plans: []PlanPreset{
			{Kind: PlanToken, Label: "按量 Token（OpenAI 兼容）", BaseURL: "https://api.stepfun.com/v1", Protocol: ProtocolOpenAIChat},
		},
		// 出处：https://platform.stepfun.com/docs/zh/api-reference/chat/chat-completion-create（2026-10-11 抓取）
		Models: []string{
			"step-5-preview",
			"step-3.7-flash",
			"step-3.5-flash",
			"step-3.5-flash-2603",
			"step-router-v1",
		},
	},
	{
		ID: "qianfan", Name: "百度千帆",
		Plans: []PlanPreset{
			{Kind: PlanToken, Label: "按量 Token（OpenAI 兼容）", BaseURL: "https://qianfan.baidubce.com/v2", Protocol: ProtocolOpenAIChat},
		},
		// 出处：https://cloud.baidu.com/doc/qianfan/s/rmh4stp0j（模型列表，2026-10-11 抓取）
		// 该页同时给出每条模型的上下文长度，窗口表用的就是它。
		Models: []string{
			"ernie-5.1",
			"ernie-5.0",
			"ernie-5.0-thinking-preview",
			"ernie-5.0-thinking-latest",
			"ernie-5.0-thinking-exp",
			"ernie-4.5-turbo-128k",
			"ernie-4.5-turbo-32k",
			"ernie-4.5-turbo-20260402",
			"ernie-4.5-turbo-vl",
			"ernie-4.5-turbo-vl-32k",
			"ernie-x1.1",
			"ernie-x1.1-preview",
			"deepseek-v4.1-flash",
			"deepseek-v4-pro",
			"deepseek-v4-pro-0813",
			"deepseek-v4-flash-0731",
			"qwen3.5-397b-a17b",
			"qwen3.5-122b-a10b",
			"qwen3.5-27b",
			"qwen3.5-35b-a3b",
			"glm-5.3-flash",
			"glm-5.3",
			"glm-5.2",
			"glm-5.1",
			"kimi-k2.6",
			"internvl3-38b",
		},
	},
	{
		ID: "hunyuan", Name: "腾讯混元",
		Plans: []PlanPreset{
			{Kind: PlanToken, Label: "按量 Token（OpenAI 兼容）", BaseURL: "https://api.hunyuan.cloud.tencent.com/v1", Protocol: ProtocolOpenAIChat},
		},
		// 出处：https://cloud.tencent.com/document/product/1729/111007（OpenAI 兼容调用示例，2026-10-11 抓取）
		// 该家另有 Anthropic 兼容入口（文档标题可见），本次未核到具体地址，故未收录。
		Models: []string{
			"hunyuan-turbos-latest",
			"hunyuan-functioncall",
			"hunyuan-vision",
			"hunyuan-lite",
		},
	},
	{
		ID: "xai", Name: "xAI Grok",
		Plans: []PlanPreset{
			{Kind: PlanToken, Label: "按量 Token（OpenAI 兼容）", BaseURL: "https://api.x.ai/v1", Protocol: ProtocolOpenAIChat},
		},
		// 出处：https://docs.x.ai/overview（2026-10-11 抓取；该页只点名了这一个模型 ID）
		Models: []string{
			"grok-4.7",
		},
	},
}

// KeyScope 把 base_url 归一成"密钥该认的那台主机"（小写 host:port，不含路径）。
//
// 用主机而不是整条 URL：同一家厂商的 /v1、/chat/completions 是不同端点同一把 key，
// 按 URL 绑会把用户每次改路径都变成"密钥丢了"。也故意不认 provider_id：自建网关
// 一个主机转发多家、同一家多个域名的情况都有，真正决定"这把 key 发给谁"的是主机。
// 解析不出来（空串、相对路径）就退回 TrimSpace 后的原值——宁可把它当成一个独立主机，
// 也不要把两行不同的配置判成同一台。
func KeyScope(baseURL string) string {
	s := strings.TrimSpace(baseURL)
	if s == "" {
		return ""
	}
	u, err := url.Parse(s)
	if err != nil || u.Host == "" {
		return strings.ToLower(s)
	}
	return strings.ToLower(u.Host)
}

// FindProvider 按 ID 查厂商预设。
func FindProvider(id string) *ProviderPreset {
	id = strings.TrimSpace(id)
	for i := range Providers {
		if Providers[i].ID == id {
			return &Providers[i]
		}
	}
	return nil
}

// ResolvePreset 依据厂商与套餐解析官方入口；用户显式填写的 base_url/model 优先。
// 返回 (baseURL, model, protocol)。
func ResolvePreset(providerID, plan, baseURL, model string) (string, string, string) {
	p := FindProvider(providerID)
	if p == nil {
		return baseURL, model, ""
	}
	kind := plan
	if kind == "" {
		kind = PlanToken
	}
	var chosen *PlanPreset
	for i := range p.Plans {
		if p.Plans[i].Kind == kind {
			chosen = &p.Plans[i]
			break
		}
	}
	if chosen == nil {
		chosen = &p.Plans[0]
	}
	if strings.TrimSpace(baseURL) == "" {
		baseURL = chosen.BaseURL
	}
	if strings.TrimSpace(model) == "" {
		model = chosen.Model
	}
	return baseURL, model, chosen.Protocol
}

// ValidProtocol 协议合法性。
func ValidProtocol(p string) bool {
	switch p {
	case ProtocolOpenAIChat, ProtocolOpenAIResponses, ProtocolAnthropic:
		return true
	}
	return false
}

// ResolveTarget 把"用户填了什么"算成"实际往哪发"：预设解析 + 协议回退，一步到位。
//
// 之前这三步（ResolvePreset → 预设协议为空则沿用已配协议 → 再兜底 openai_chat）
// 在启动装配、设置保存、连接自测三处各抄一遍，而密钥作用域恰恰要问"到底是哪台主机"：
// 三份里任何一份漏了兜底，就会算出两个不同的 scope。收到这里来，三处只问一次。
func ResolveTarget(providerID, plan, baseURL, model, protocol string) (string, string, string) {
	base, model, presetProto := ResolvePreset(providerID, plan, baseURL, model)
	if presetProto != "" {
		protocol = presetProto
	}
	if !ValidProtocol(protocol) {
		protocol = ProtocolOpenAIChat
	}
	return base, model, protocol
}

// New 协议工厂：按线协议构造对应客户端（全部自研实现）。timeoutSecs<=0 时用 60s。
func New(protocol, baseURL, apiKey, model string, temperature float64, maxTokens, timeoutSecs int) Client {
	if timeoutSecs <= 0 {
		timeoutSecs = 60
	}
	timeout := time.Duration(timeoutSecs) * time.Second
	switch protocol {
	case ProtocolOpenAIResponses:
		return NewResponses(baseURL, apiKey, model, temperature, maxTokens, timeout)
	case ProtocolAnthropic:
		return NewAnthropic(baseURL, apiKey, model, temperature, maxTokens, timeout)
	default:
		return NewGLM(baseURL, apiKey, model, temperature, maxTokens, timeout)
	}
}
