package llm

import (
	"net/url"
	"strings"
	"time"
)

const (
	ProtocolOpenAIChat      = "openai_chat"
	ProtocolOpenAIResponses = "openai_responses"
	ProtocolAnthropic       = "anthropic"
)

const (
	PlanToken  = "token"
	PlanCoding = "coding"
	PlanAgent  = "agent"
)

type PlanPreset struct {
	Kind     string `json:"kind"`
	Label    string `json:"label"`
	BaseURL  string `json:"base_url"`
	Protocol string `json:"protocol"`
	Model    string `json:"model"`
}

type ProviderPreset struct {
	ID          string       `json:"id"`
	Name        string       `json:"name"`
	Plans       []PlanPreset `json:"plans"`
	Recommended bool         `json:"recommended,omitempty"`
	Models      []string     `json:"models"`
}

var Providers = []ProviderPreset{
	{
		ID: "tokendance", Name: "TokenDance 词元跳动（聚合）", Recommended: true,
		Plans: []PlanPreset{
			{Kind: PlanToken, Label: "按量 Token（OpenAI 兼容）", BaseURL: "https://tokendance.space/gateway/v1", Protocol: ProtocolOpenAIChat},
			{Kind: PlanAgent, Label: "Anthropic 兼容入口", BaseURL: "https://tokendance.space/gateway/v1", Protocol: ProtocolAnthropic},
		},
		Models: []string{
			"qwen3.8-flash",
			"glm-5.3",
		},
	},
	{
		ID: "deepseek", Name: "DeepSeek 深度求索", Recommended: true,
		Plans: []PlanPreset{
			{Kind: PlanToken, Label: "按量 Token（快速版，OpenAI 兼容）", BaseURL: "https://api.deepseek.com/v1", Protocol: ProtocolOpenAIChat},
			{Kind: PlanCoding, Label: "编程模式（旗舰版，复杂任务）", BaseURL: "https://api.deepseek.com/v1", Protocol: ProtocolOpenAIChat},
		},
		Models: []string{
			"deepseek-v4-pro",
			"deepseek-flash",
		},
	},
	{
		ID: "zhipu", Name: "智谱 GLM",
		Plans: []PlanPreset{
			{Kind: PlanToken, Label: "按量 Token（OpenAI 兼容）", BaseURL: "https://open.bigmodel.cn/api/paas/v4", Protocol: ProtocolOpenAIChat},
			{Kind: PlanCoding, Label: "GLM Coding Plan 编程套餐（OpenAI 兼容）", BaseURL: "https://open.bigmodel.cn/api/coding/paas/v4", Protocol: ProtocolOpenAIChat},
			{Kind: PlanAgent, Label: "Anthropic 兼容入口（Claude Code 同款）", BaseURL: "https://open.bigmodel.cn/api/anthropic", Protocol: ProtocolAnthropic},
		},
		Models: []string{
			"glm-5.3",
			"glm-5.3-flash",
		},
	},
	{
		ID: "zhipu-intl", Name: "Z.ai（智谱国际站）",
		Plans: []PlanPreset{
			{Kind: PlanToken, Label: "按量 Token（OpenAI 兼容）", BaseURL: "https://api.z.ai/api/paas/v4", Protocol: ProtocolOpenAIChat},
			{Kind: PlanCoding, Label: "GLM Coding Plan 编程套餐（OpenAI 兼容）", BaseURL: "https://api.z.ai/api/coding/paas/v4", Protocol: ProtocolOpenAIChat},
			{Kind: PlanAgent, Label: "Anthropic 兼容入口（Claude Code 同款）", BaseURL: "https://api.z.ai/api/anthropic", Protocol: ProtocolAnthropic},
		},
		Models: []string{
			"glm-5.3",
			"glm-5.3-flash",
		},
	},
	{
		ID: "moonshot", Name: "月之暗面 Kimi",
		Plans: []PlanPreset{
			{Kind: PlanToken, Label: "按量 Token（OpenAI 兼容）", BaseURL: "https://api.moonshot.cn/v1", Protocol: ProtocolOpenAIChat},
			{Kind: PlanCoding, Label: "Kimi Coding（Anthropic 兼容；托管版经 Kimi Code 登录自动配置）", BaseURL: "https://api.moonshot.cn/anthropic", Protocol: ProtocolAnthropic},
		},
		Models: []string{
			"kimi-k3",
			"kimi-k2.7-code",
			"kimi-k2.7-code-highspeed",
			"kimi-k2.6",
		},
	},
	{
		ID: "moonshot-intl", Name: "Kimi（国际站）",
		Plans: []PlanPreset{
			{Kind: PlanToken, Label: "按量 Token（OpenAI 兼容）", BaseURL: "https://api.moonshot.ai/v1", Protocol: ProtocolOpenAIChat},
			{Kind: PlanCoding, Label: "Kimi Coding（Anthropic 兼容）", BaseURL: "https://api.moonshot.ai/anthropic", Protocol: ProtocolAnthropic},
		},
		Models: []string{
			"kimi-k3",
			"kimi-k2.7-code",
			"kimi-k2.7-code-highspeed",
			"kimi-k2.6",
		},
	},
	{
		ID: "qwen", Name: "阿里云百炼（通义 Qwen）",
		Plans: []PlanPreset{
			{Kind: PlanToken, Label: "按量 Token（OpenAI 兼容）", BaseURL: "https://dashscope.aliyuncs.com/compatible-mode/v1", Protocol: ProtocolOpenAIChat},
			{Kind: PlanCoding, Label: "百炼 Coding Plan（Anthropic 兼容；Key 为 sk-sp- 开头，与按量不互通）", BaseURL: "https://coding.dashscope.aliyuncs.com/anthropic", Protocol: ProtocolAnthropic},
			{Kind: PlanAgent, Label: "百炼 Coding Plan（OpenAI 兼容；一个 Key 聚合千问/GLM/Kimi/MiniMax）", BaseURL: "https://coding.dashscope.aliyuncs.com/v1", Protocol: ProtocolOpenAIChat},
		},
		Models: []string{
			"qwen3.8-max",
			"qwen3.7-plus",
			"qwen3.8-flash",
			"qwen3-coder-plus",
		},
	},
	{
		ID: "qianwen", Name: "千问AI平台（Qwen）",
		Plans: []PlanPreset{
			{Kind: PlanToken, Label: "按量 Token（OpenAI 兼容）", BaseURL: "https://maas.qianwenaiapi.com/compatible-mode/v1", Protocol: ProtocolOpenAIChat},
			{Kind: PlanCoding, Label: "Token Plan 编程订阅（Anthropic 兼容）", BaseURL: "https://token-plan.maas.qianwenaiapi.com/apps/anthropic", Protocol: ProtocolAnthropic},
			{Kind: PlanAgent, Label: "Token Plan 编程订阅（OpenAI 兼容）", BaseURL: "https://token-plan.maas.qianwenaiapi.com/compatible-mode/v1", Protocol: ProtocolOpenAIChat},
		},
		Models: []string{
			"qwen3.8-max",
			"qwen3.7-plus",
			"qwen3.8-flash",
			"qwen3.7-max",
		},
	},
	{
		ID: "volc", Name: "火山方舟（豆包）",
		Plans: []PlanPreset{
			{Kind: PlanToken, Label: "按量 Token（OpenAI 兼容）", BaseURL: "https://ark.cn-beijing.volces.com/api/v3", Protocol: ProtocolOpenAIChat},
			{Kind: PlanCoding, Label: "方舟 Coding Plan（Anthropic 兼容，适配 Claude Code）", BaseURL: "https://ark.cn-beijing.volces.com/api/coding", Protocol: ProtocolAnthropic},
			{Kind: PlanAgent, Label: "方舟 Coding Plan（OpenAI 兼容入口）", BaseURL: "https://ark.cn-beijing.volces.com/api/coding/v3", Protocol: ProtocolOpenAIChat},
		},
		Models: []string{
			"doubao-seed-evolving",
			"doubao-seed-2.1-pro",
			"doubao-seed-2.1-lite",
			"doubao-seed-2.0-mini",
			"deepseek-v4-pro",
			"deepseek-v4.1-flash",
			"deepseek-v4-flash",
			"glm-5.3",
			"glm-5.3-flash",
			"kimi-k3",
			"kimi-k2.8-preview",
			"kimi-k2.7-code",
			"minimax-m3",
			"ark-code-latest",
		},
	},
	{
		ID: "byteplus", Name: "BytePlus ModelArk（字节国际站）",
		Plans: []PlanPreset{
			{Kind: PlanToken, Label: "按量 Token（OpenAI 兼容）", BaseURL: "https://ark.ap-southeast.bytepluses.com/api/v3", Protocol: ProtocolOpenAIChat},
			{Kind: PlanCoding, Label: "ModelArk Coding Plan（Anthropic 兼容，适配 Claude Code）", BaseURL: "https://ark.ap-southeast.bytepluses.com/api/coding", Protocol: ProtocolAnthropic},
			{Kind: PlanAgent, Label: "ModelArk Coding Plan（OpenAI 兼容入口）", BaseURL: "https://ark.ap-southeast.bytepluses.com/api/coding/v3", Protocol: ProtocolOpenAIChat},
		},
		Models: []string{
			"dola-seed-2-1-turbo",
			"seed-2-0-pro",
			"seed-2-0-lite",
			"seed-2-0-mini",
			"seed-2-0-code",
			"deepseek-v4-1-flash",
			"deepseek-v4-pro",
			"deepseek-v4-flash",
			"glm-5-3-flash",
			"glm-5-2",
			"kimi-k2",
			"ark-code-latest",
		},
	},
	{
		ID: "minimax", Name: "MiniMax（国内站）",
		Plans: []PlanPreset{
			{Kind: PlanToken, Label: "按量 Token（OpenAI 兼容，国内站）", BaseURL: "https://api.minimaxi.com/v1", Protocol: ProtocolOpenAIChat},
			{Kind: PlanCoding, Label: "MiniMax Coding Plan（Anthropic 兼容，国内站）", BaseURL: "https://api.minimaxi.com/anthropic", Protocol: ProtocolAnthropic},
		},
		Models: []string{
			"MiniMax-M3.1-Flash-Preview",
			"MiniMax-M3",
			"MiniMax-M2.7",
			"MiniMax-M2.7-highspeed",
			"MiniMax-M2.5",
			"MiniMax-M2.5-highspeed",
			"MiniMax-M2.1",
			"MiniMax-M2.1-highspeed",
			"MiniMax-M2",
		},
	},
	{
		ID: "minimax-intl", Name: "MiniMax（国际站）",
		Plans: []PlanPreset{
			{Kind: PlanToken, Label: "按量 Token（OpenAI 兼容，国际站）", BaseURL: "https://api.minimax.io/v1", Protocol: ProtocolOpenAIChat},
			{Kind: PlanCoding, Label: "MiniMax Coding Plan（Anthropic 兼容，国际站）", BaseURL: "https://api.minimax.io/anthropic", Protocol: ProtocolAnthropic},
		},
		Models: []string{
			"MiniMax-M3.1-Flash-Preview",
			"MiniMax-M3",
			"MiniMax-M2.7",
			"MiniMax-M2.7-highspeed",
			"MiniMax-M2.5",
			"MiniMax-M2.5-highspeed",
			"MiniMax-M2.1",
			"MiniMax-M2.1-highspeed",
			"MiniMax-M2",
		},
	},
	{
		ID: "stepfun", Name: "阶跃星辰 StepFun",
		Plans: []PlanPreset{
			{Kind: PlanToken, Label: "按量 Token（OpenAI 兼容）", BaseURL: "https://api.stepfun.com/v1", Protocol: ProtocolOpenAIChat},
		},
		Models: []string{
			"step-5-preview",
			"step-3.7-flash",
			"step-3.5-flash",
			"step-3.5-flash-2603",
			"step-router-v1",
		},
	},
	{
		ID: "stepfun-intl", Name: "StepFun（国际站）",
		Plans: []PlanPreset{
			{Kind: PlanToken, Label: "按量 Token（OpenAI 兼容）", BaseURL: "https://api.stepfun.ai/v1", Protocol: ProtocolOpenAIChat},
			{Kind: PlanCoding, Label: "Step Plan（Anthropic 兼容，适配 Claude Code）", BaseURL: "https://api.stepfun.ai/step_plan", Protocol: ProtocolAnthropic},
			{Kind: PlanAgent, Label: "Step Plan（OpenAI 兼容入口）", BaseURL: "https://api.stepfun.ai/step_plan/v1", Protocol: ProtocolOpenAIChat},
		},
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
		ID: "qianfan-intl", Name: "百度千帆（国际站）",
		Plans: []PlanPreset{
			{Kind: PlanToken, Label: "按量 Token（OpenAI 兼容）", BaseURL: "https://api.baiduqianfan.ai/v1", Protocol: ProtocolOpenAIChat},
		},
		Models: []string{
			"ernie-5.1",
			"ernie-5.0",
			"ernie-4.5-turbo-128k",
			"ernie-4.5-turbo-32k",
			"deepseek-v4-pro",
			"deepseek-v4-pro-0813",
			"deepseek-v4-flash",
			"deepseek-v4-flash-0731",
			"deepseek-v3.2",
			"glm-5.2",
			"glm-5.2-fast",
			"glm-5.1",
			"glm-5",
			"kimi-k2.6",
			"mimo-v2.5",
		},
	},
	{
		ID: "tokenhub", Name: "腾讯云 TokenHub",
		Plans: []PlanPreset{
			{Kind: PlanToken, Label: "按量 Token（OpenAI 兼容）", BaseURL: "https://tokenhub.tencentmaas.com/v1", Protocol: ProtocolOpenAIChat},
			{Kind: PlanCoding, Label: "TokenHub Coding Plan（Anthropic 兼容，适配 Claude Code）", BaseURL: "https://api.lkeap.cloud.tencent.com/coding/anthropic", Protocol: ProtocolAnthropic},
			{Kind: PlanAgent, Label: "TokenHub Coding Plan（OpenAI 兼容入口）", BaseURL: "https://api.lkeap.cloud.tencent.com/coding/v3", Protocol: ProtocolOpenAIChat},
		},
		Models: []string{
			"hy4-preview",
			"hy3",
			"hy-mt2-pro",
			"hy-mt2-plus",
			"hy-mt2-lite",
			"deepseek-v4-pro",
			"deepseek-v4.1-flash",
			"deepseek-v4-flash",
			"glm-5.3",
			"glm-5.3-flash",
			"glm-5.2",
			"glm-5.1",
			"glm-5-turbo",
			"glm-5",
			"kimi-k3",
			"kimi-k2.8-preview",
			"kimi-k2.7-code",
			"kimi-k2.7-code-highspeed",
			"kimi-k2.6",
			"minimax-m3",
			"minimax-m2.7",
			"qwen3.5-plus",
			"qwen3.5-flash",
			"mimo-v2.6-pro",
			"mimo-v2.6-flash",
			"mimo-v2.5-pro",
			"step-5-preview",
		},
	},
	{
		ID: "tokenhub-intl", Name: "腾讯云 TokenHub（国际站）",
		Plans: []PlanPreset{
			{Kind: PlanToken, Label: "按量 Token（OpenAI 兼容）", BaseURL: "https://tokenhub-intl.tencentmaas.com/v1", Protocol: ProtocolOpenAIChat},
			{Kind: PlanCoding, Label: "Token Plan 订阅（Anthropic 兼容）", BaseURL: "https://tokenhub-intl.tencentcloudmaas.com/plan/anthropic", Protocol: ProtocolAnthropic},
			{Kind: PlanAgent, Label: "Token Plan 订阅（OpenAI 兼容）", BaseURL: "https://tokenhub-intl.tencentcloudmaas.com/plan/v3", Protocol: ProtocolOpenAIChat},
		},
		Models: []string{
			"hy4-preview",
			"glm-5.3-flash",
			"glm-5.2",
			"kimi-k3",
			"kimi-k2.6",
			"deepseek-v4-pro-202606",
			"deepseek-v4-flash-202605",
			"minimax-m3",
			"mimo-v2.6-flash",
			"auto",
		},
	},
	{
		ID: "openai", Name: "OpenAI",
		Plans: []PlanPreset{
			{Kind: PlanToken, Label: "按量 Token（Chat Completions）", BaseURL: "https://api.openai.com/v1", Protocol: ProtocolOpenAIChat},
			{Kind: PlanAgent, Label: "Responses API", BaseURL: "https://api.openai.com/v1", Protocol: ProtocolOpenAIResponses},
		},
		Models: []string{
			"gpt-4.1",
		},
	},
	{
		ID: "anthropic", Name: "Anthropic",
		Plans: []PlanPreset{
			{Kind: PlanToken, Label: "按量 Token（Messages API）", BaseURL: "https://api.anthropic.com/v1", Protocol: ProtocolAnthropic},
		},
		Models: []string{
			"claude-opus-5-5",
			"claude-sonnet-5-5",
			"claude-haiku-5-5",
			"claude-fable-5-1",
			"claude-sonnet-4-5",
		},
	},
	{
		ID: "openrouter", Name: "OpenRouter（聚合）",
		Plans: []PlanPreset{
			{Kind: PlanToken, Label: "按量 Token", BaseURL: "https://openrouter.ai/api/v1", Protocol: ProtocolOpenAIChat},
		},
		Models: []string{
			"openrouter/auto",
			"~openai/gpt-sol-latest",
			"~anthropic/claude-sonnet-latest",
		},
	},
	{
		ID: "gemini", Name: "Google Gemini",
		Plans: []PlanPreset{
			{Kind: PlanToken, Label: "按量 Token（OpenAI 兼容）", BaseURL: "https://generativelanguage.googleapis.com/v1beta/openai/", Protocol: ProtocolOpenAIChat},
		},
		Models: []string{
			"gemini-3.8-flash",
			"gemini-3.5-flash-lite",
			"gemini-3.1-pro-preview",
		},
	},
	{
		ID: "xai", Name: "xAI Grok",
		Plans: []PlanPreset{
			{Kind: PlanToken, Label: "按量 Token（OpenAI 兼容）", BaseURL: "https://api.x.ai/v1", Protocol: ProtocolOpenAIChat},
		},
		Models: []string{
			"grok-4.7",
		},
	},
	{
		ID: "mulerouter", Name: "MuleRouter（聚合）",
		Plans: []PlanPreset{
			{Kind: PlanToken, Label: "按量 Token（OpenAI 兼容）", BaseURL: "https://api.mulerouter.ai/v1", Protocol: ProtocolOpenAIChat},
			{Kind: PlanAgent, Label: "Anthropic 兼容入口", BaseURL: "https://api.mulerouter.ai/v1", Protocol: ProtocolAnthropic},
		},
		Models: []string{
			"gpt-5.5",
			"gpt-5.4-mini",
			"gpt-5.4-nano",
			"grok-4",
			"grok-4-20-non-reasoning",
			"grok-code-fast-1",
			"qwen3.7-max",
			"qwen3.6-max-preview",
			"qwen3.6-plus",
			"qwen3.6-flash",
			"qwen3.5-plus",
			"qwen3.5-flash",
			"qwen3-max",
			"qwen3-max-thinking",
			"qwen-plus",
			"qwen-flash",
			"glm-5.1",
		},
	},
}

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

func FindProvider(id string) *ProviderPreset {
	id = strings.TrimSpace(id)
	for i := range Providers {
		if Providers[i].ID == id {
			return &Providers[i]
		}
	}
	return nil
}

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

func ValidProtocol(p string) bool {
	switch p {
	case ProtocolOpenAIChat, ProtocolOpenAIResponses, ProtocolAnthropic:
		return true
	}
	return false
}

func ResolveTarget(providerID, plan, baseURL, model, protocol string) (string, string, string) {
	base, model, presetProto := ResolvePreset(providerID, plan, baseURL, model)
	// An explicit protocol wins: when the user or an existing config states a
	// protocol, a plan default must not silently override it (otherwise a
	// custom endpoint is forced onto another API shape). Only fall back to the
	// plan value when no protocol is set.
	if !ValidProtocol(protocol) {
		protocol = presetProto
	}
	if !ValidProtocol(protocol) {
		protocol = ProtocolOpenAIChat
	}
	return base, model, protocol
}

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
