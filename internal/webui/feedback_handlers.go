package webui

import (
	"context"
	"encoding/base64"
	"fmt"
	"net/http"
	"os"
	"runtime"
	"strconv"
	"strings"
	"time"

	"gleam/internal/agent"
	"gleam/internal/buildinfo"
	"gleam/internal/harness/feedback"
	"gleam/internal/llm"
	"gleam/pkg/types"
)

const (
	maxFeedbackTextRunes         = 20000
	maxFeedbackAttachments       = 5
	feedbackBodyCap        int64 = 40 << 20
	feedbackDeliverTimeout       = 15 * time.Second
)

type feedbackSubmitRequest struct {
	Kind        string `json:"kind"`
	Text        string `json:"text"`
	TaskID      string `json:"task_id,omitempty"`
	Attachments []struct {
		Data string `json:"data"`
	} `json:"attachments,omitempty"`
}

func (s *Server) handleFeedbackSubmit(w http.ResponseWriter, r *http.Request) {
	var req feedbackSubmitRequest
	if err := readJSONCap(r, feedbackBodyCap, &req); err != nil {
		writeErr(w, 400, "参数解析失败或请求过大")
		return
	}
	kind, err := parseFeedbackKind(req.Kind)
	if err != nil {
		writeErr(w, 400, "%v", err)
		return
	}
	text := strings.TrimSpace(req.Text)
	if text == "" {
		writeErr(w, 400, "请先写一句描述：没有文字的截图，收到也不知道该改什么")
		return
	}
	if len([]rune(text)) > maxFeedbackTextRunes {
		writeErr(w, 400, "描述过长（%d 字，上限 %d 字）——把要提交的文字放进截图或文件里更合适",
			len([]rune(text)), maxFeedbackTextRunes)
		return
	}
	if len(req.Attachments) > maxFeedbackAttachments {
		writeErr(w, 400, "截图最多 %d 张（当前 %d 张）", maxFeedbackAttachments, len(req.Attachments))
		return
	}

	id := feedback.NewID()
	var stored []types.FeedbackAttachment
	for _, a := range req.Attachments {
		data, err := decodeFeedbackImage(a.Data)
		if err != nil {
			s.discardFeedback(id)
			writeErr(w, 400, "%v", err)
			return
		}
		name, mime, err := s.feedback.SaveAttachment(id, data)
		if err != nil {
			s.discardFeedback(id)
			writeErr(w, 400, "%v", err)
			return
		}
		stored = append(stored, types.FeedbackAttachment{Name: name, Mime: mime, Bytes: len(data)})
	}

	f := &types.Feedback{
		ID:          id,
		Kind:        kind,
		Text:        text,
		Attachments: stored,
		Context:     s.feedbackContext(req.TaskID),
		CreatedAt:   time.Now().UTC(),
		Delivery:    types.FeedbackLocalOnly,
	}
	if err := s.feedback.Save(f); err != nil {
		s.discardFeedback(id)
		writeErr(w, 500, "反馈没能存下来：%v", err)
		return
	}
	s.deliverFeedback(f)
	writeJSON(w, 201, f)
}

func (s *Server) handleFeedbackContext(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, 200, map[string]any{
		"context": s.feedbackContext(r.URL.Query().Get("task_id")),
		"remote":  s.feedbackRemoteName(),
	})
}

func (s *Server) handleFeedbackList(w http.ResponseWriter, r *http.Request) {
	limit := 50
	if v := r.URL.Query().Get("limit"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 && n <= 200 {
			limit = n
		}
	}
	list, skipped, err := s.feedback.List(limit)
	if err != nil {
		writeErr(w, 500, "反馈列表读不动：%v", err)
		return
	}
	if skipped > 0 {
		fmt.Fprintf(os.Stderr, "[gleam] %d 条反馈读不动，已跳过\n", skipped)
	}
	writeJSON(w, 200, map[string]any{
		"count":    len(list),
		"skipped":  skipped,
		"remote":   s.feedbackRemoteName(),
		"feedback": list,
	})
}

func (s *Server) feedbackRemoteName() string {
	if sink := s.feedbackSink(); sink != nil {
		return sink.Name()
	}
	return ""
}

func (s *Server) handleFeedbackDelete(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	f, err := s.feedback.Read(id)
	if err != nil {
		writeErr(w, 500, "反馈读不动：%v", err)
		return
	}
	if f == nil {
		writeErr(w, 404, "没有这条反馈")
		return
	}
	if err := s.feedback.Delete(id); err != nil {
		writeErr(w, 500, "删除失败：%v", err)
		return
	}
	writeJSON(w, 200, map[string]any{"deleted": id})
}

func (s *Server) handleFeedbackAttachment(w http.ResponseWriter, r *http.Request) {
	name := r.URL.Query().Get("name")
	data, mime, err := s.feedback.ReadAttachment(name)
	if err != nil {
		writeErr(w, 500, "截图读不动：%v", err)
		return
	}
	if data == nil {
		writeErr(w, 404, "没有这张截图")
		return
	}
	w.Header().Set("Content-Type", mime)
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Content-Length", fmt.Sprint(len(data)))
	_, _ = w.Write(data)
}

func (s *Server) handleFeedbackResend(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	f, err := s.feedback.Read(id)
	if err != nil {
		writeErr(w, 500, "反馈读不动：%v", err)
		return
	}
	if f == nil {
		writeErr(w, 404, "没有这条反馈")
		return
	}
	if s.feedbackSink() == nil {
		writeErr(w, 400, "还没配远端投递：先在「我的 → 账号」填 Supabase 项目地址与 anon key")
		return
	}
	s.deliverFeedback(f)
	writeJSON(w, 200, f)
}

func (s *Server) deliverFeedback(f *types.Feedback) {
	sink := s.feedbackSink()
	if sink == nil || f.Delivery == types.FeedbackSent {
		return
	}
	out, redacted := feedback.Redact(f, s.feedbackSecrets())
	ctx, cancel := context.WithTimeout(context.Background(), feedbackDeliverTimeout)
	defer cancel()
	if err := sink.Send(ctx, out); err != nil {
		f.Delivery = types.FeedbackFailed
		f.DeliveryNote = fmt.Sprintf("已存本机，未送达 %s：%s",
			sink.Name(), types.Shorten(err.Error(), 160))
	} else {
		f.Delivery = types.FeedbackSent
		f.DeliveryNote = fmt.Sprintf("已送达 %s", sink.Name())
	}
	if redacted > 0 {
		f.DeliveryNote += fmt.Sprintf("（发出的副本里替换了 %d 处机密片段）", redacted)
	}
	if err := s.feedback.Save(f); err != nil {
		fmt.Fprintf(os.Stderr, "[gleam] 反馈 %s 的投递状态没写回去：%v\n", f.ID, err)
	}
}

func (s *Server) feedbackSink() feedback.Sink {
	if s.Agent.Creds == nil {
		return nil
	}
	c := s.Agent.Creds.GetCloudConfig()
	if c == nil {
		return nil
	}
	target := feedback.SupabaseTarget{URL: c.SupabaseURL, AnonKey: c.SupabaseAnonKey}
	if !target.Configured() {
		return nil
	}
	return feedback.NewSupabase(target, func(host string, nbytes int) {
		s.Agent.Gate.RecordEgress("feedback", host, nbytes)
	})
}

func (s *Server) feedbackSecrets() []string {
	cfg := s.Agent.Cfg
	out := []string{cfg.Workspace, cfg.DataDir}
	if key, _ := agent.LLMKeyFor(cfg, s.Agent.Creds, cfg.LLM.BaseURL, ""); key != "" {
		out = append(out, key)
	}
	if cfg.LLM.APIKey != "" {
		out = append(out, cfg.LLM.APIKey)
	}
	return out
}

func (s *Server) feedbackContext(taskID string) types.FeedbackContext {
	cfg := s.Agent.Cfg
	ctx := types.FeedbackContext{
		AppVersion: buildinfo.Version,
		GoVersion:  runtime.Version(),
		OS:         runtime.GOOS + "/" + runtime.GOARCH,
		Model:      strings.TrimSpace(cfg.LLM.Model),
		LLMHost:    llm.KeyScope(cfg.LLM.BaseURL),
	}
	res := s.recentTaskResult(taskID)
	if res == nil {
		return ctx
	}
	ctx.TaskID = res.TaskID
	ctx.TaskStatus = res.Status
	for i := len(res.Steps) - 1; i >= 0; i-- {
		if res.Steps[i].Status == types.StepFailed && res.Steps[i].Tool != "" {
			ctx.FailedTool = res.Steps[i].Tool
			break
		}
	}
	return ctx
}

func (s *Server) recentTaskResult(taskID string) *types.GoalResult {
	if taskID != "" {
		if status, res := s.Agent.TaskStatus(taskID); res != nil {
			return res
		} else if status != "" {
			return &types.GoalResult{TaskID: taskID, Status: status}
		}
		if t, ok := s.archivedTask(taskID); ok {
			return t.Result
		}
		return &types.GoalResult{TaskID: taskID}
	}
	if res := s.newestMemoryTask(); res != nil {
		return res
	}
	archived, _, err := agent.ListTaskResults(s.Agent.Cfg.DataDir, 1)
	if err != nil || len(archived) == 0 {
		return nil
	}
	return archived[0]
}

func (s *Server) newestMemoryTask() *types.GoalResult {
	s.mu.Lock()
	defer s.mu.Unlock()
	var newest *taskInfo
	for _, t := range s.tasks {
		if newest == nil || t.Started.After(newest.Started) {
			newest = t
		}
	}
	if newest == nil {
		return nil
	}
	if newest.Result != nil {
		return newest.Result
	}
	return &types.GoalResult{TaskID: newest.ID, Status: newest.Status}
}

func (s *Server) discardFeedback(id string) {
	if err := s.feedback.Delete(id); err != nil {
		fmt.Fprintf(os.Stderr, "[gleam] 反馈 %s 回滚失败，feedback/ 里可能残留它的截图：%v\n", id, err)
	}
}

func parseFeedbackKind(raw string) (types.FeedbackKind, error) {
	switch strings.TrimSpace(raw) {
	case string(types.FeedbackBug):
		return types.FeedbackBug, nil
	case string(types.FeedbackSuggestion):
		return types.FeedbackSuggestion, nil
	}
	return "", fmt.Errorf("反馈类型只能是「问题」或「建议」（当前 %q）", raw)
}

func decodeFeedbackImage(raw string) ([]byte, error) {
	s := strings.TrimSpace(raw)
	if i := strings.Index(s, "base64,"); i >= 0 {
		s = s[i+len("base64,"):]
	}
	if s == "" {
		return nil, fmt.Errorf("截图是空的")
	}
	data, err := base64.StdEncoding.DecodeString(strings.Join(strings.Fields(s), ""))
	if err != nil {
		data, err = base64.RawStdEncoding.DecodeString(strings.Join(strings.Fields(s), ""))
		if err != nil {
			return nil, fmt.Errorf("截图没认出来（不是 base64）")
		}
	}
	return data, nil
}
