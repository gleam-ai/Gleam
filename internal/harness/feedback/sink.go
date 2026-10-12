package feedback

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"gleam/pkg/types"
)

type Sink interface {
	Send(ctx context.Context, f *types.Feedback) error
	Name() string
}

type SupabaseTarget struct {
	URL     string
	AnonKey string
	Table   string
}

func (t SupabaseTarget) Configured() bool {
	return strings.TrimSpace(t.URL) != "" && strings.TrimSpace(t.AnonKey) != ""
}

func (t SupabaseTarget) table() string {
	if s := strings.TrimSpace(t.Table); s != "" {
		return s
	}
	return FeedbackTable
}

const FeedbackTable = "feedback_reports"

type supabaseSink struct {
	target SupabaseTarget
	client *http.Client
	egress EgressFunc
}

type EgressFunc func(host string, nbytes int)

func NewSupabase(target SupabaseTarget, egress EgressFunc) Sink {
	return &supabaseSink{target: target, client: &http.Client{Timeout: 12 * time.Second}, egress: egress}
}

func (s *supabaseSink) Name() string { return "supabase" }

func (s *supabaseSink) Send(ctx context.Context, f *types.Feedback) error {
	body, err := json.Marshal(feedbackRow(f))
	if err != nil {
		return fmt.Errorf("整理投递内容失败: %w", err)
	}
	url := strings.TrimRight(s.target.URL, "/") + "/rest/v1/" + s.target.table()
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("apikey", s.target.AnonKey)
	req.Header.Set("Authorization", "Bearer "+s.target.AnonKey)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Prefer", "return=minimal")
	if s.egress != nil && req.URL != nil {
		s.egress(req.URL.Host, len(body))
	}
	resp, err := s.client.Do(req)
	if err != nil {
		return fmt.Errorf("连不上 %s：%v", s.target.table(), err)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if resp.StatusCode >= 400 {
		msg := strings.TrimSpace(string(raw))
		if resp.StatusCode == http.StatusNotFound || strings.Contains(msg, "42P01") || strings.Contains(msg, "does not exist") {
			return fmt.Errorf("远端没有表 %q：请先在 Supabase 建表（建表语句见 README「反馈与建议」一节）", s.target.table())
		}
		return fmt.Errorf("%d %s", resp.StatusCode, types.Shorten(msg, 200))
	}
	return nil
}

func feedbackRow(f *types.Feedback) map[string]any {
	return map[string]any{
		"id":          f.ID,
		"kind":        string(f.Kind),
		"text":        f.Text,
		"app_version": f.Context.AppVersion,
		"go_version":  f.Context.GoVersion,
		"os":          f.Context.OS,
		"model":       f.Context.Model,
		"llm_host":    f.Context.LLMHost,
		"task_id":     f.Context.TaskID,
		"task_status": string(f.Context.TaskStatus),
		"failed_tool": f.Context.FailedTool,
		"attachments": f.Attachments,
		"created_at":  f.CreatedAt.Format(time.RFC3339),
	}
}
