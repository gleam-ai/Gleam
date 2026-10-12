package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"gleam/internal/llm"
	"gleam/pkg/types"
)

type llmReviewer struct {
	LLM    llm.Client
	TaskID string
}

const reviewTimeout = 15 * time.Second

type reviewVerdict struct {
	OK     bool   `json:"ok"`
	Reason string `json:"reason"`
}

func (r *llmReviewer) Review(userGoal, tool string, args map[string]any) (bool, string) {
	if r == nil || r.LLM == nil {
		return true, ""
	}
	argText, err := json.Marshal(args)
	if err != nil {
		argText = []byte("{}")
	}
	sys := strings.Join([]string{
		"你是动作审核器。" + llm.MarkerReview,
		"只回答一个问题：用户要的是这件事，而助手现在要执行这个动作——这算合理解读吗？",
		"只输出 JSON：{\"ok\":true或false,\"reason\":\"一句中文说明\"}",
		"",
		"## 判定为不合理（ok=false）的典型情况",
		"1. 销毁或外传数据（删除、覆盖、上传到外部、发送邮件消息）；",
		"2. 削弱安全配置（改权限、关校验、写凭据到不安全位置）；",
		"3. 跨越信任边界（写到用户目标之外的范围、访问无关的系统位置）；",
		"4. 绕过既有审查（拼接命令、间接执行、隐藏真实意图）。",
		"",
		"## 注意",
		"- 拿不准就判 false：多标一次只是让人多点一下确认，漏一次就是事故。",
		"- 只读、查看、列目录这类动作默认合理。",
		"- 不要因为动作没见过就否决，只看是否超出用户目标的合理范围。",
	}, "\n")
	req := llm.ChatRequest{
		System: sys,
		Messages: []llm.Message{{Role: llm.RoleUser, Content: fmt.Sprintf(
			"用户目标：%s\n\n待执行动作：%s\n参数：%s",
			types.Shorten(userGoal, 200), tool, types.Shorten(string(argText), 400))}},
		Temperature: 0, MaxTokens: 150, TaskID: r.TaskID,
	}
	ctx, cancel := context.WithTimeout(context.Background(), reviewTimeout)
	defer cancel()
	text, err := r.LLM.Chat(ctx, req)
	if err != nil {
		return true, ""
	}
	raw, err := extractJSON(text)
	if err != nil {
		return false, "审核输出无法解析，交人工确认"
	}
	var v reviewVerdict
	if err := json.Unmarshal(raw, &v); err != nil {
		return false, "审核输出无法解析，交人工确认"
	}
	return v.OK, strings.TrimSpace(v.Reason)
}

const deepReviewTimeout = 30 * time.Second

func (a *Agent) deepReviewPlan(ctx context.Context, taskID, goal string, plan *types.Plan) (bool, string) {
	if a == nil || a.LLM == nil || plan == nil || len(plan.Steps) == 0 {
		return true, ""
	}
	var b strings.Builder
	for i, s := range plan.Steps {
		args, _ := json.Marshal(s.Args)
		fmt.Fprintf(&b, "%d. [%s] %s\n   参数：%s\n",
			i+1, s.Tool, types.Shorten(s.Description, 120), types.Shorten(string(args), 240))
	}
	sys := strings.Join([]string{
		"你是计划审核器。" + llm.MarkerReview,
		"只回答一个问题：这一整段计划，是不是在**用户目标范围内**、以合理方式达成它？",
		"只输出 JSON：{\"ok\":true或false,\"reason\":\"一句中文说明\"}",
		"",
		"## 判 false 的典型情况",
		"1. 整段计划在做用户没要的事（顺手删东西、顺手改配置、把数据发到外部）；",
		"2. 单步都正常，合起来却越过了目标边界（先读 A、再拿 A 里的凭据去 B）；",
		"3. 有更小、更直接的做法，却选了会破坏现状的那条；",
		"4. 缺少兑现目标的关键一步（声称完成但计划里没有任何验证动作）。",
		"",
		"## 注意",
		"- 拿不准就判 false：多标一次只是让人多点一下确认，漏一次就是事故。",
		"- 计划多不等于错，只在取向不对时才否决。",
	}, "\n")
	req := llm.ChatRequest{
		System: sys,
		Messages: []llm.Message{{Role: llm.RoleUser, Content: fmt.Sprintf(
			"用户目标：%s\n\n计划（共 %d 步）：\n%s",
			types.Shorten(goal, 300), len(plan.Steps), types.Shorten(b.String(), 1800))}},
		Temperature: 0, MaxTokens: 200, TaskID: taskID,
	}

	cctx, cancel := context.WithTimeout(ctx, deepReviewTimeout)
	defer cancel()
	text, err := a.LLM.Chat(cctx, req)
	if err != nil {
		return true, ""
	}
	raw, err := extractJSON(text)
	if err != nil {
		return false, "深度扫描的输出没法解析，交你确认"
	}
	var v reviewVerdict
	if err := json.Unmarshal(raw, &v); err != nil {
		return false, "深度扫描的输出没法解析，交你确认"
	}
	return v.OK, strings.TrimSpace(v.Reason)
}
