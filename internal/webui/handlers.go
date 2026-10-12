package webui

import (
	"context"
	"embed"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"net/http"
	"os"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"gleam/internal/agent"
	"gleam/internal/buildinfo"
	"gleam/internal/harness/skill"
	"gleam/internal/market"
	"gleam/pkg/types"
)

//go:embed static
var staticFS embed.FS

func (s *Server) Handler() http.Handler {
	return s.guard(s.routes())
}

func (s *Server) routes() http.Handler {
	mux := http.NewServeMux()

	sub, _ := fs.Sub(staticFS, "static")
	fileServer := http.FileServer(http.FS(sub))
	mux.HandleFunc("GET /", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/" {
			http.NotFound(w, r)
			return
		}
		s.serveIndex(w, r)
	})
	mux.Handle("GET /assets/", noCache(http.StripPrefix("/assets/", fileServer)))

	mux.HandleFunc("GET /api/info", s.handleInfo)
	mux.HandleFunc("GET /api/events", s.handleEvents)

	mux.HandleFunc("POST /api/goals", s.handleGoalSubmit)
	mux.HandleFunc("GET /api/goals", s.handleGoalList)
	mux.HandleFunc("GET /api/goals/{id}", s.handleGoalGet)
	mux.HandleFunc("POST /api/goals/{id}/cancel", s.handleGoalCancel)
	mux.HandleFunc("GET /api/goals/{id}/diff", s.handleGoalDiff)
	mux.HandleFunc("POST /api/goals/{id}/revert", s.handleGoalRevert)

	mux.HandleFunc("GET /api/approvals", s.handleApprovalList)
	mux.HandleFunc("POST /api/approvals/{id}", s.handleApprovalResolve)

	mux.HandleFunc("GET /api/tools", s.handleToolsList)
	mux.HandleFunc("POST /api/tools/call", s.handleToolCall)
	mux.HandleFunc("POST /api/tools/permission", s.handleToolPermission)

	mux.HandleFunc("GET /api/memory", s.handleMemorySearch)
	mux.HandleFunc("POST /api/memory", s.handleMemorySave)
	mux.HandleFunc("DELETE /api/memory/{id}", s.handleMemoryDelete)

	mux.HandleFunc("GET /api/skills", s.handleSkillsList)
	mux.HandleFunc("POST /api/skills", s.handleSkillSave)
	mux.HandleFunc("POST /api/skills/{name}/run", s.handleSkillRun)
	mux.HandleFunc("POST /api/skills/{name}/enabled", s.handleSkillEnabled)
	mux.HandleFunc("DELETE /api/skills/{name}", s.handleSkillDelete)

	mux.HandleFunc("GET /api/schedules", s.handleScheduleList)
	mux.HandleFunc("POST /api/schedules", s.handleScheduleCreate)
	mux.HandleFunc("POST /api/schedules/{name}/notify", s.handleScheduleNotify)
	mux.HandleFunc("POST /api/schedules/{name}/enabled", s.handleScheduleEnabled)
	mux.HandleFunc("DELETE /api/schedules/{name}", s.handleScheduleDelete)

	mux.HandleFunc("POST /api/hooks/{name}", s.handleHookTrigger)
	mux.HandleFunc("POST /api/heartbeat", s.handleHeartbeat)
	mux.HandleFunc("POST /api/show-window", s.handleShowWindow)

	mux.HandleFunc("GET /api/go-status", s.handleGoStatus)
	mux.HandleFunc("POST /api/go-status/install", s.handleGoInstall)
	mux.HandleFunc("POST /api/go-status", s.handleGoStatusSet)

	mux.HandleFunc("GET /api/security/audit", s.handleSecurityAudit)

	mux.HandleFunc("GET /api/connections", s.handleConnections)

	mux.HandleFunc("GET /api/cues", s.handleCues)
	mux.HandleFunc("POST /api/cues/unsuppress", s.handleCueUnsuppress)
	mux.HandleFunc("POST /api/cues/{id}/dismiss", s.handleCueDismiss)
	mux.HandleFunc("POST /api/cues/{id}/adopt", s.handleCueAdopt)

	mux.HandleFunc("GET /api/settings", s.handleSettingsGet)
	mux.HandleFunc("POST /api/settings", s.handleSettingsSave)
	mux.HandleFunc("POST /api/llm/test", s.handleLLMTest)
	mux.HandleFunc("POST /api/llm/models", s.handleLLMModels)
	mux.HandleFunc("GET /api/models", s.handleModelsList)
	mux.HandleFunc("POST /api/models", s.handleModelAdd)
	mux.HandleFunc("PUT /api/models/{id}", s.handleModelUpdate)
	mux.HandleFunc("DELETE /api/models/{id}", s.handleModelDelete)
	mux.HandleFunc("POST /api/models/{id}/default", s.handleModelSetDefault)
	mux.HandleFunc("POST /api/models/{id}/activate", s.handleModelActivate)
	mux.HandleFunc("GET /api/ssh/hosts", s.handleSSHHosts)
	mux.HandleFunc("DELETE /api/goals/{id}", s.handleGoalDelete)
	mux.HandleFunc("GET /api/network", s.handleNetworkInfo)
	mux.HandleFunc("GET /api/context", s.handleContextGet)
	mux.HandleFunc("POST /api/context/compress", s.handleContextCompress)
	mux.HandleFunc("POST /api/context/clear", s.handleContextClear)
	mux.HandleFunc("POST /api/conversation/reset", s.handleConversationReset)

	mux.HandleFunc("GET /api/conversations", s.handleConversationList)
	mux.HandleFunc("POST /api/conversations", s.handleConversationCreate)
	mux.HandleFunc("GET /api/conversations/{id}", s.handleConversationGet)
	mux.HandleFunc("PATCH /api/conversations/{id}", s.handleConversationRename)
	mux.HandleFunc("DELETE /api/conversations/{id}", s.handleConversationDelete)
	mux.HandleFunc("POST /api/conversations/{id}/activate", s.handleConversationActivate)

	mux.HandleFunc("GET /api/spaces", s.handleSpaceList)
	mux.HandleFunc("POST /api/spaces", s.handleSpaceCreate)
	mux.HandleFunc("PATCH /api/spaces/{id}", s.handleSpaceRename)
	mux.HandleFunc("POST /api/spaces/{id}/activate", s.handleSpaceActivate)
	mux.HandleFunc("DELETE /api/spaces/{id}", s.handleSpaceDelete)

	mux.HandleFunc("GET /api/update/check", s.handleUpdateCheck)
	mux.HandleFunc("POST /api/update/apply", s.handleUpdateApply)

	mux.HandleFunc("GET /api/import/scan", s.handleImportScan)
	mux.HandleFunc("POST /api/import/apply", s.handleImportApply)

	mux.HandleFunc("GET /api/workspace", s.handleWorkspaceGet)
	mux.HandleFunc("POST /api/workspace", s.handleWorkspaceSet)
	mux.HandleFunc("POST /api/workspace/clear", s.handleWorkspaceClear)
	mux.HandleFunc("GET /api/worktrees", s.handleWorktreeList)
	mux.HandleFunc("DELETE /api/worktrees/{id}", s.handleWorktreeRemove)
	mux.HandleFunc("GET /api/fs", s.handleFsBrowse)

	mux.HandleFunc("GET /api/providers", s.handleProviders)
	mux.HandleFunc("GET /api/market/mcp", s.handleMarketMCP)
	mux.HandleFunc("POST /api/market/mcp/install", s.handleMarketMCPInstall)
	mux.HandleFunc("GET /api/market/sources", s.handleMarketSources)
	mux.HandleFunc("GET /api/market/runtimes", s.handleMarketRuntimes)
	mux.HandleFunc("GET /api/market/skills", s.handleMarketSkills)
	mux.HandleFunc("POST /api/market/skills/install", s.handleMarketSkillInstall)
	mux.HandleFunc("GET /api/mcp", s.handleMCPList)
	mux.HandleFunc("POST /api/mcp", s.handleMCPInstallCustom)
	mux.HandleFunc("DELETE /api/mcp/{name}", s.handleMCPRemove)
	mux.HandleFunc("POST /api/mcp/{name}/reconnect", s.handleMCPRetry)
	mux.HandleFunc("POST /api/mcp/{name}/enabled", s.handleMCPToggle)

	mux.HandleFunc("GET /api/roles", s.handleRolesList)
	mux.HandleFunc("GET /api/growth", s.handleGrowthStats)
	mux.HandleFunc("GET /api/growth/recent", s.handleGrowthRecent)

	mux.HandleFunc("GET /api/readiness", s.handleReadiness)

	mux.HandleFunc("GET /api/geo", s.handleGEOHistory)
	mux.HandleFunc("POST /api/geo/analyze", s.handleGEOAnalyze)
	mux.HandleFunc("DELETE /api/geo/history", s.handleGEOClear)

	mux.HandleFunc("GET /api/account", s.handleAccountGet)
	mux.HandleFunc("POST /api/account/configure", s.handleAccountConfigure)
	mux.HandleFunc("POST /api/account/signup", s.handleAccountSignUp)
	mux.HandleFunc("POST /api/account/signin", s.handleAccountSignIn)
	mux.HandleFunc("POST /api/account/signout", s.handleAccountSignOut)
	mux.HandleFunc("POST /api/account/oauth", s.handleAccountOAuth)
	mux.HandleFunc("GET /api/local-data", s.handleLocalData)

	mux.HandleFunc("POST /api/feedback", s.handleFeedbackSubmit)
	mux.HandleFunc("GET /api/feedback", s.handleFeedbackList)
	mux.HandleFunc("GET /api/feedback/context", s.handleFeedbackContext)
	mux.HandleFunc("POST /api/feedback/{id}/resend", s.handleFeedbackResend)
	mux.HandleFunc("DELETE /api/feedback/{id}", s.handleFeedbackDelete)
	mux.HandleFunc("GET /api/feedback/attachment", s.handleFeedbackAttachment)

	mux.HandleFunc("GET /api/onboarding", s.handleOnboardingGet)
	mux.HandleFunc("POST /api/onboarding", s.handleOnboardingSave)

	// 守护进程（后台运行：注册/启动/停止系统服务，实现 24/7 执行）
	mux.HandleFunc("GET /api/daemon", s.handleDaemonStatus)
	mux.HandleFunc("POST /api/daemon", s.handleDaemonAction)

	return mux
}

func noCache(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-cache, must-revalidate")
		next.ServeHTTP(w, r)
	})
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func writeErr(w http.ResponseWriter, status int, format string, args ...any) {
	writeJSON(w, status, map[string]any{"error": fmt.Sprintf(format, args...)})
}

func readJSON(r *http.Request, dst any) error {
	return readJSONCap(r, 2<<20, dst)
}

func readJSONCap(r *http.Request, limit int64, dst any) error {
	defer r.Body.Close()
	r.Body = io.NopCloser(io.LimitReader(r.Body, limit+1))
	return json.NewDecoder(r.Body).Decode(dst)
}

func (s *Server) handleInfo(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, 200, map[string]any{
		"name":    "gleam",
		"version": buildinfo.Version,
		"model":   s.Agent.LLMName(),
		"tools":   len(s.Agent.ToolNames()),
		"memory":  s.Agent.MemoryCount(),
		"now":     time.Now(),
	})
}

func (s *Server) handleEvents(w http.ResponseWriter, r *http.Request) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		writeErr(w, 500, "当前连接不支持流式推送")
		return
	}
	w.Header().Set("Content-Type", "text/event-stream; charset=utf-8")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no")

	ch := s.subscribe()
	defer s.unsubscribe(ch)

	fmt.Fprint(w, ": connected\n\n")
	flusher.Flush()

	heartbeat := time.NewTicker(15 * time.Second)
	defer heartbeat.Stop()
	for {
		select {
		case <-r.Context().Done():
			return
		case ev := <-ch:
			fmt.Fprintf(w, "event: %s\ndata: %s\n\n", ev.Type, ev.Data)
			flusher.Flush()
		case <-heartbeat.C:
			fmt.Fprint(w, ": ping\n\n")
			flusher.Flush()
		}
	}
}

type goalSubmitBody struct {
	Goal           string            `json:"goal"`
	Mode           string            `json:"mode"`
	TaskMode       string            `json:"task_mode"`
	References     []types.Reference `json:"references,omitempty"`
	Role           string            `json:"role,omitempty"`
	ConversationID string            `json:"conversation_id,omitempty"`
}

func (s *Server) handleGoalSubmit(w http.ResponseWriter, r *http.Request) {
	var body goalSubmitBody
	if err := readJSON(r, &body); err != nil {
		writeErr(w, 400, "请求体格式无效")
		return
	}
	if len([]rune(body.Goal)) < 2 {
		writeErr(w, 400, "目标太短，请至少输入 2 个字")
		return
	}
	req := types.GoalRequest{Goal: body.Goal, References: body.References, Mode: body.Mode, TaskMode: types.TaskMode(body.TaskMode), Role: body.Role, ConversationID: body.ConversationID}
	if err := agent.ValidateGoalRequest(req); err != nil {
		writeErr(w, 400, "%v", err)
		return
	}
	taskID := types.NewID()
	req.TaskID = taskID
	ctx, cancel := context.WithCancel(context.Background())

	t := &taskInfo{ID: taskID, Goal: body.Goal, Mode: body.Mode, TaskMode: body.TaskMode, Status: types.GoalRunning, Started: time.Now(), Cancel: cancel}
	s.mu.Lock()
	s.tasks[taskID] = t
	s.mu.Unlock()

	go func() {
		result := s.Agent.RunGoal(ctx, req)
		cancel()
		if err := agent.SaveTaskResult(s.Agent.Cfg.DataDir, result); err != nil {
			fmt.Fprintf(os.Stderr, "[gleam] 界面任务未存档：%v\n", err)
		}
		s.mu.Lock()
		t.Status = result.Status
		t.Result = result
		s.pruneTasksLocked(taskID)
		s.mu.Unlock()
		s.broadcast(newEvent("completed", result))
	}()
	resp := map[string]any{"task_id": taskID, "status": "running"}
	if s.Agent.Cfg.LLM.Provider != "mock" {
		if key, storedHost := agent.LLMKeyFor(s.Agent.Cfg, s.Agent.Creds, s.Agent.Cfg.LLM.BaseURL, ""); key == "" {
			if storedHost != "" {
				resp["warning"] = fmt.Sprintf("当前接入主机没有可用密钥（本机存着的是发给 %s 的那把）。密钥按接入主机绑定，请为现在的厂商重新填一次。", storedHost)
			} else {
				resp["warning"] = "未配置 API Key，模型调用将失败（401）。请到「设置 → 模型」填写后重试，或在启动时使用 --mock-llm 离线体验。"
			}
		}
	}
	writeJSON(w, 200, resp)
}

const maxRetainedTasks = 200

func (s *Server) pruneTasksLocked(keep string) {
	if len(s.tasks) <= maxRetainedTasks {
		return
	}
	type rec struct {
		id      string
		started time.Time
	}
	var done []rec
	for id, t := range s.tasks {
		if t.Status != types.GoalRunning && id != keep {
			done = append(done, rec{id, t.Started})
		}
	}
	sort.Slice(done, func(i, j int) bool { return done[i].started.Before(done[j].started) })
	for i := 0; i < len(done) && len(s.tasks) > maxRetainedTasks; i++ {
		delete(s.tasks, done[i].id)
	}
}

const listArchiveLimit = 100

func (s *Server) handleGoalList(w http.ResponseWriter, _ *http.Request) {
	s.mu.Lock()
	list := make([]*taskInfo, 0, len(s.tasks))
	seen := make(map[string]bool, len(s.tasks))
	for _, t := range s.tasks {
		list = append(list, t.snapshot())
		seen[t.ID] = true
	}
	s.mu.Unlock()
	archived, skipped, err := agent.ListTaskResults(s.Agent.Cfg.DataDir, listArchiveLimit)
	if err != nil {
		fmt.Fprintf(os.Stderr, "[gleam] 任务归档列表读不动：%v\n", err)
	}
	if skipped > 0 {
		fmt.Fprintf(os.Stderr, "[gleam] %d 份任务归档读不动，已跳过\n", skipped)
	}
	for _, g := range archived {
		if g == nil || seen[g.TaskID] {
			continue
		}
		list = append(list, archivedTaskInfo(g))
	}
	sort.Slice(list, func(i, j int) bool { return list[i].Started.After(list[j].Started) })
	writeJSON(w, 200, map[string]any{"count": len(list), "goals": list})
}

func (s *Server) handleGoalGet(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	s.mu.Lock()
	t, ok := s.tasks[id]
	if ok {
		t = t.snapshot()
	}
	s.mu.Unlock()
	if !ok {
		if archived, has := s.archivedTask(id); has {
			writeJSON(w, 200, archived)
			return
		}
		writeErr(w, 404, "任务 %q 不存在", id)
		return
	}
	writeJSON(w, 200, t)
}

var safeIDChars = regexp.MustCompile(`^[A-Za-z0-9_-]+$`)

func (s *Server) archivedTask(id string) (*taskInfo, bool) {
	g, err := agent.ReadTaskResult(s.Agent.Cfg.DataDir, id)
	if err != nil {
		fmt.Fprintf(os.Stderr, "[gleam] 任务归档 %s 读不动：%v\n", id, err)
		return nil, false
	}
	if g == nil {
		return nil, false
	}
	return archivedTaskInfo(g), true
}

func archivedTaskInfo(g *types.GoalResult) *taskInfo {
	return &taskInfo{
		ID: g.TaskID, Goal: g.Goal, Mode: g.Mode, TaskMode: string(g.TaskMode),
		Status: g.Status, Result: g, Started: g.StartedAt,
	}
}

func (s *Server) handleGoalCancel(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	s.mu.Lock()
	t, ok := s.tasks[id]
	if ok && t.Cancel != nil {
		t.Cancel()
	}
	s.mu.Unlock()
	_ = s.Agent.CancelTask(id)
	s.dropTaskApprovals(id, "任务已取消")
	writeJSON(w, 200, map[string]any{"task_id": id, "cancelled": ok})
}

func (s *Server) handleGoalDiff(w http.ResponseWriter, r *http.Request) {
	view, err := s.Agent.TaskDiff(r.PathValue("id"), r.URL.Query().Get("path"))
	if err != nil {
		writeErr(w, 400, "%v", err)
		return
	}
	writeJSON(w, 200, view)
}

type goalRevertBody struct {
	Path string `json:"path"`
}

func (s *Server) handleGoalRevert(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	var body goalRevertBody
	if err := readJSON(r, &body); err != nil {
		writeErr(w, 400, "请求体无效")
		return
	}
	res, err := s.Agent.TaskRevert(id, body.Path)
	if err != nil {
		writeErr(w, 400, "%v", err)
		return
	}
	if archived, has := s.archivedTask(id); has {
		s.mu.Lock()
		if t, ok := s.tasks[id]; ok {
			t.Result = archived.Result
		}
		s.mu.Unlock()
		res["refreshed"] = true
	}
	writeJSON(w, 200, res)
}

func (s *Server) handleApprovalList(w http.ResponseWriter, _ *http.Request) {
	s.mu.Lock()
	defer s.mu.Unlock()
	list := make([]map[string]any, 0, len(s.approvals))
	for _, wt := range s.approvals {
		list = append(list, wt.view())
	}
	writeJSON(w, 200, map[string]any{"count": len(list), "approvals": list})
}

func (s *Server) handleApprovalResolve(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	var body struct {
		Approved bool   `json:"approved"`
		Note     string `json:"note"`
	}
	if err := readJSON(r, &body); err != nil {
		writeErr(w, 400, "参数解析失败")
		return
	}
	if !s.resolve(id, types.ApprovalResponse{Approved: body.Approved, Note: body.Note}) {
		writeErr(w, 404, "审批请求 %q 不存在或已裁决", id)
		return
	}
	writeJSON(w, 200, map[string]any{"id": id, "resolved": true})
}

func (s *Server) handleToolsList(w http.ResponseWriter, _ *http.Request) {
	list := s.Agent.ToolList()
	writeJSON(w, 200, map[string]any{"count": len(list), "tools": list})
}

type toolCallBody struct {
	Name string         `json:"name"`
	Args map[string]any `json:"args"`
}

func (s *Server) handleToolCall(w http.ResponseWriter, r *http.Request) {
	var body toolCallBody
	if err := readJSON(r, &body); err != nil || body.Name == "" {
		writeErr(w, 400, "需要 name")
		return
	}
	call, err := s.Agent.DirectToolCall(r.Context(), body.Name, body.Args, s.OnApproval)
	if err != nil {
		writeErr(w, 400, "%v", err)
		return
	}
	writeJSON(w, 200, call)
}

func (s *Server) handleMemorySearch(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query().Get("q")
	if q == "" {
		writeErr(w, 400, "需要 q")
		return
	}
	k, _ := strconv.Atoi(r.URL.Query().Get("k"))
	hits := s.Agent.MemorySearch(q, k)
	writeJSON(w, 200, map[string]any{"count": len(hits), "hits": hits})
}

func (s *Server) handleMemorySave(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Content string   `json:"content"`
		Tags    []string `json:"tags"`
	}
	if err := readJSON(r, &body); err != nil || body.Content == "" {
		writeErr(w, 400, "需要 content")
		return
	}
	id, err := s.Agent.MemorySave(body.Content, body.Tags)
	if err != nil {
		writeErr(w, 500, "%v", err)
		return
	}
	writeJSON(w, 200, map[string]any{"id": id, "saved": true})
}

func (s *Server) handleMemoryDelete(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if !safeIDChars.MatchString(id) {
		writeErr(w, 400, "非法记忆 ID")
		return
	}
	if !s.Agent.MemoryDelete(id) {
		writeErr(w, 404, "记忆 %q 不存在或已删除", id)
		return
	}
	writeJSON(w, 200, map[string]any{"id": id, "deleted": true})
}

func (s *Server) handleSkillsList(w http.ResponseWriter, _ *http.Request) {
	list := s.Agent.SkillList()
	writeJSON(w, 200, map[string]any{"count": len(list), "skills": list})
}

func (s *Server) handleSkillSave(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Name        string       `json:"name"`
		Description string       `json:"description"`
		Params      []string     `json:"params"`
		Steps       []types.Step `json:"steps"`
	}
	if err := readJSON(r, &body); err != nil {
		writeErr(w, 400, "参数解析失败")
		return
	}
	version, err := s.Agent.SkillSave(skill.Skill{
		Name: body.Name, Description: body.Description, Params: body.Params, Steps: body.Steps,
	})
	if err != nil {
		writeErr(w, 400, "%v", err)
		return
	}
	writeJSON(w, 200, map[string]any{"name": body.Name, "version": version, "saved": true})
}

func (s *Server) handleSkillRun(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")
	var body struct {
		Params map[string]string `json:"params"`
	}
	if err := readJSON(r, &body); err != nil && err != io.EOF {
		writeErr(w, 400, "参数解析失败")
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()
	out, err := s.Agent.SkillRun(ctx, name, body.Params)
	if err != nil {
		writeErr(w, 400, "%v", err)
		return
	}
	writeJSON(w, 200, out)
}

func (s *Server) handleSkillEnabled(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")
	var body struct {
		Enabled *bool `json:"enabled"`
	}
	if err := readJSON(r, &body); err != nil || body.Enabled == nil {
		writeErr(w, 400, "需要 enabled（true 启用 / false 停用）")
		return
	}
	res, err := s.Agent.SkillSetEnabled(name, *body.Enabled)
	if err != nil {
		writeErr(w, 404, "%v", err)
		return
	}
	writeJSON(w, 200, res)
}

func (s *Server) handleSkillDelete(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")
	if err := s.Agent.SkillDelete(name); err != nil {
		writeErr(w, 400, "%v", err)
		return
	}
	writeJSON(w, 200, map[string]any{"name": name, "deleted": true})
}

func (s *Server) handleScheduleList(w http.ResponseWriter, _ *http.Request) {
	jobs := s.Agent.ScheduleList()
	writeJSON(w, 200, map[string]any{"count": len(jobs), "jobs": jobs})
}

func (s *Server) handleScheduleCreate(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Name        string `json:"name"`
		Goal        string `json:"goal"`
		When        string `json:"when"`
		Cron        string `json:"cron"`
		IntervalSec int    `json:"interval_sec"`
		Mode        string `json:"mode"`
		Notify      string `json:"notify"`
	}
	if err := readJSON(r, &body); err != nil {
		writeErr(w, 400, "参数解析失败")
		return
	}
	job, err := s.Agent.ScheduleCreateWithNotify(body.Name, body.Goal, body.Mode, body.When, body.Cron, body.IntervalSec, body.Notify)
	if err != nil {
		writeErr(w, 400, "%v", err)
		return
	}
	writeJSON(w, 200, job)
}

func (s *Server) handleScheduleNotify(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")
	var body struct {
		Notify string `json:"notify"`
	}
	if err := readJSON(r, &body); err != nil {
		writeErr(w, 400, "参数解析失败")
		return
	}
	job, err := s.Agent.ScheduleSetNotify(name, body.Notify)
	if err != nil {
		writeErr(w, 400, "%v", err)
		return
	}
	writeJSON(w, 200, job)
}

func (s *Server) handleScheduleEnabled(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")
	var body struct {
		Enabled *bool `json:"enabled"`
	}
	if err := readJSON(r, &body); err != nil || body.Enabled == nil {
		writeErr(w, 400, "参数解析失败，需要 {\"enabled\": true|false}")
		return
	}
	job, err := s.Agent.ScheduleSetEnabled(name, *body.Enabled)
	if err != nil {
		writeErr(w, 400, "%v", err)
		return
	}
	writeJSON(w, 200, job)
}

func (s *Server) handleScheduleDelete(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")
	ok, err := s.Agent.ScheduleDelete(name)
	if err != nil {
		writeErr(w, 500, "%v", err)
		return
	}
	if !ok {
		writeErr(w, 404, "任务 %q 不存在", name)
		return
	}
	writeJSON(w, 200, map[string]any{"name": name, "deleted": true})
}

func (s *Server) handleHookTrigger(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")
	ok, err := s.Agent.ScheduleTrigger(name)
	if err != nil {
		writeErr(w, 404, "%v", err)
		return
	}
	writeJSON(w, 200, map[string]any{"name": name, "triggered": ok})
}

func (s *Server) handleHeartbeat(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, 200, map[string]any{"ok": true})
}

func (s *Server) handleShowWindow(w http.ResponseWriter, _ *http.Request) {
	if s.ShowWindowFunc != nil {
		go s.ShowWindowFunc()
		writeJSON(w, 200, map[string]any{"ok": true})
		return
	}
	writeJSON(w, 200, map[string]any{"ok": false, "reason": "no window manager"})
}

func (s *Server) handleSettingsGet(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, 200, s.Agent.SettingsView())
}

func (s *Server) handleSettingsSave(w http.ResponseWriter, r *http.Request) {
	var patch map[string]any
	if err := readJSON(r, &patch); err != nil {
		writeErr(w, 400, "参数解析失败")
		return
	}
	view, err := s.Agent.ApplySettings(patch)
	if err != nil {
		writeErr(w, 400, "%v", err)
		return
	}
	writeJSON(w, 200, view)
}

func (s *Server) handleLLMTest(w http.ResponseWriter, r *http.Request) {
	var override map[string]any
	_ = readJSON(r, &override)
	if override == nil {
		override = map[string]any{}
	}
	writeJSON(w, 200, s.Agent.TestLLMConnection(override))
}

func (s *Server) handleLLMModels(w http.ResponseWriter, r *http.Request) {
	var override map[string]any
	_ = readJSON(r, &override)
	if override == nil {
		override = map[string]any{}
	}
	writeJSON(w, 200, s.Agent.ListLLMModels(override))
}

func (s *Server) handleModelsList(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, 200, map[string]any{
		"models": s.Agent.ModelsList(),
		"active": s.Agent.ActiveModel(),
	})
}

func (s *Server) handleModelAdd(w http.ResponseWriter, r *http.Request) {
	var body map[string]any
	if err := readJSON(r, &body); err != nil {
		writeErr(w, 400, "参数解析失败")
		return
	}
	view, err := s.Agent.ModelAdd(body)
	if err != nil {
		writeErr(w, 400, "%v", err)
		return
	}
	writeJSON(w, 200, view)
}

func (s *Server) handleModelUpdate(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	var body map[string]any
	if err := readJSON(r, &body); err != nil {
		writeErr(w, 400, "参数解析失败")
		return
	}
	view, err := s.Agent.ModelUpdate(id, body)
	if err != nil {
		writeErr(w, 400, "%v", err)
		return
	}
	writeJSON(w, 200, view)
}

func (s *Server) handleModelDelete(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	view, err := s.Agent.ModelDelete(id)
	if err != nil {
		writeErr(w, 400, "%v", err)
		return
	}
	writeJSON(w, 200, view)
}

func (s *Server) handleModelSetDefault(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	view, err := s.Agent.ModelSetDefault(id)
	if err != nil {
		writeErr(w, 400, "%v", err)
		return
	}
	writeJSON(w, 200, view)
}

func (s *Server) handleModelActivate(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	view, err := s.Agent.SetActiveModel(id)
	if err != nil {
		writeErr(w, 400, "%v", err)
		return
	}
	writeJSON(w, 200, view)
}

func (s *Server) handleContextGet(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, 200, s.Agent.ContextView())
}

func (s *Server) handleContextCompress(w http.ResponseWriter, _ *http.Request) {
	if s.Agent.Mem == nil {
		writeErr(w, 400, "记忆系统未启用")
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	compressed := s.Agent.Mem.Compress(s.Agent.CompressSummarizer(ctx))
	writeJSON(w, 200, map[string]any{"compressed": compressed, "context": s.Agent.ContextView()})
}

func (s *Server) handleContextClear(w http.ResponseWriter, _ *http.Request) {
	if s.Agent.Mem == nil {
		writeErr(w, 400, "记忆系统未启用")
		return
	}
	s.Agent.Mem.ClearSummary()
	writeJSON(w, 200, map[string]any{"cleared": true, "context": s.Agent.ContextView()})
}

func (s *Server) handleConversationReset(w http.ResponseWriter, _ *http.Request) {
	s.Agent.ResetConversation()
	s.mu.Lock()
	for id, t := range s.tasks {
		if t.Status != types.GoalRunning {
			delete(s.tasks, id)
		}
	}
	s.mu.Unlock()
	writeJSON(w, 200, map[string]any{"reset": true, "context": s.Agent.ContextView()})
}

func (s *Server) handleProviders(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, 200, map[string]any{"count": len(s.Agent.ProvidersView()), "providers": s.Agent.ProvidersView()})
}

func (s *Server) handleMarketMCP(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query().Get("q")
	list := s.Agent.MarketMCP(q)

	remote, note := s.marketSearch(r.Context(), q, r.URL.Query().Get("refresh") == "1")
	remote = market.RankRemote(remote, q)
	rtByName := map[string]market.RuntimeStatus{}
	for _, r := range market.Runtimes() {
		rtByName[r.Name] = r
	}
	decorateRuntime := func(command string) (string, bool, string) {
		name := market.RuntimeFor(command)
		if name == "" {
			return "", true, ""
		}
		r, ok := rtByName[name]
		if !ok {
			return name, true, ""
		}
		return name, r.Found, r.Why
	}
	pick, why := s.marketNPMSource(r.Context())
	for _, p := range remote {
		rtName, rtFound, rtWhy := decorateRuntime(p.Command)
		list = append(list, map[string]any{
			"id": p.ID, "name": p.Name, "desc": p.Desc, "command": p.Command,
			"params": p.Params, "trust": p.Trust, "tags": p.Tags,
			"installed":      s.Agent.MCPInstalledByArgs(p.PkgRef),
			"remote":         true,
			"source":         p.Source,
			"version":        p.Version,
			"pkg_ref":        p.PkgRef,
			"homepage":       p.Homepage,
			"installable":    p.Installable,
			"unsupported":    p.Unsupported,
			"base_args":      marketInstallArgs(pick, strings.HasPrefix(p.Command, "npx"), p.BaseArgs),
			"npm_source":     pick.ID,
			"npm_source_why": why,
			"runtime":        rtName,
			"runtime_found":  rtFound,
			"runtime_why":    rtWhy,
		})
	}
	writeJSON(w, 200, map[string]any{
		"count": len(list), "presets": list,
		"remote_count": len(remote), "source_note": note,
		"npm_source": pick.ID, "npm_source_why": why,
	})
}

func (s *Server) handleMarketMCPInstall(w http.ResponseWriter, r *http.Request) {
	var body struct {
		ID     string            `json:"id"`
		Params map[string]string `json:"params"`
		Trust  string            `json:"trust"`
		Force  bool              `json:"force"`
	}
	if err := readJSON(r, &body); err != nil || body.ID == "" {
		writeErr(w, 400, "需要 id")
		return
	}
	if _, err := market.FindMCP(body.ID); err != nil {
		res, rErr := s.installRemoteMCP(r.Context(), body.ID, body.Params, body.Trust, body.Force)
		if rErr != nil {
			writeInstallErr(w, rErr)
			return
		}
		writeJSON(w, 200, res)
		return
	}
	res, err := s.Agent.MCPInstallPreset(body.ID, body.Params, body.Trust, body.Force)
	if err != nil {
		writeInstallErr(w, err)
		return
	}
	writeJSON(w, 200, res)
}

func (s *Server) installRemoteMCP(ctx context.Context, id string, params map[string]string, trust string, force bool) (map[string]any, error) {
	p, note, found := s.lookupRemotePreset(ctx, id)
	if found {
		if !p.Installable {
			return nil, fmt.Errorf("这条装不了：%s", p.Unsupported)
		}
		if miss, missing := market.RuntimeMissing(p.Command); missing {
			return nil, fmt.Errorf("本机没有 %s，装不了它：%s", miss.Name, miss.Why)
		}
		name := remoteMCPName(p.ID)
		if p.Kind == "http" {
			headers, hErr := market.BuildHeaders(p, params)
			if hErr != nil {
				return nil, hErr
			}
			if len(headers) == 0 {
				headers = nil
			}
			return s.Agent.MCPInstallRemote(name, p.URL, headers, trust, force)
		}
		pick, _ := s.marketNPMSource(ctx)
		args := marketInstallArgs(pick, strings.HasPrefix(p.Command, "npx"), p.BaseArgs)
		_, envVals := market.SplitParams(p.Params, params)
		env := map[string]string{}
		for k, v := range envVals {
			if strings.TrimSpace(v) != "" {
				env[k] = strings.TrimSpace(v)
			}
		}
		for _, pm := range p.Params {
			if pm.Kind == "env" && pm.Required && env[pm.Key] == "" {
				return nil, fmt.Errorf("缺少必填环境变量 %q（%s）", pm.Key, pm.Label)
			}
		}
		if len(env) == 0 {
			env = nil
		}
		return s.Agent.MCPInstallCustom(name, p.Command, args, env, trust, force)
	}
	if note != "" {
		return nil, fmt.Errorf("目录源当前不可用（%s），拿不到 %q 的安装信息", note, id)
	}
	return nil, fmt.Errorf("远端目录里没有 %q", id)
}

func remoteMCPName(id string) string {
	base := id
	if i := strings.LastIndexAny(base, "/"); i >= 0 {
		base = base[i+1:]
	}
	base = strings.ToLower(base)
	base = strings.Map(func(r rune) rune {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9', r == '-', r == '_':
			return r
		}
		return '-'
	}, base)
	base = strings.Trim(base, "-")
	if base == "" || len(base) > 32 {
		base = "remote"
	}
	return base
}

func (s *Server) handleMarketSkills(w http.ResponseWriter, r *http.Request) {
	list := s.Agent.MarketSkills(r.URL.Query().Get("q"))
	writeJSON(w, 200, map[string]any{
		"count": len(list), "presets": list, "categories": market.SkillCategories(),
	})
}

func (s *Server) handleMarketSkillInstall(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Name  string `json:"name"`
		Force bool   `json:"force"`
	}
	if err := readJSON(r, &body); err != nil || body.Name == "" {
		writeErr(w, 400, "需要 name")
		return
	}
	version, err := s.Agent.SkillInstallPreset(body.Name, body.Force)
	if err != nil {
		writeInstallErr(w, err)
		return
	}
	writeJSON(w, 200, map[string]any{"name": body.Name, "version": version, "installed": true})
}

func writeInstallErr(w http.ResponseWriter, err error) {
	if errors.Is(err, agent.ErrAlreadyInstalled) {
		writeErr(w, 409, "%v", err)
		return
	}
	writeErr(w, 400, "%v", err)
}

func (s *Server) handleMCPList(w http.ResponseWriter, _ *http.Request) {
	list := s.Agent.MCPList()
	writeJSON(w, 200, map[string]any{"count": len(list), "mcp": list})
}

func (s *Server) handleMCPInstallCustom(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Name    string            `json:"name"`
		Command string            `json:"command"`
		Args    []string          `json:"args"`
		Env     map[string]string `json:"env"`
		Trust   string            `json:"trust"`
		Force   bool              `json:"force"`
	}
	if err := readJSON(r, &body); err != nil || body.Command == "" {
		writeErr(w, 400, "需要 command")
		return
	}
	res, err := s.Agent.MCPInstallCustom(body.Name, body.Command, body.Args, body.Env, body.Trust, body.Force)
	if err != nil {
		writeInstallErr(w, err)
		return
	}
	writeJSON(w, 200, res)
}

func (s *Server) handleMCPToggle(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")
	var body struct {
		Enabled *bool `json:"enabled"`
	}
	if err := readJSON(r, &body); err != nil || body.Enabled == nil {
		writeErr(w, 400, "需要 enabled（true 启用 / false 停用）")
		return
	}
	res, err := s.Agent.MCPSetEnabled(name, *body.Enabled)
	if err != nil {
		writeErr(w, 404, "%v", err)
		return
	}
	writeJSON(w, 200, res)
}

func (s *Server) handleMCPRetry(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")
	res, err := s.Agent.MCPRetry(name)
	if err != nil {
		writeErr(w, 404, "%v", err)
		return
	}
	writeJSON(w, 200, res)
}

func (s *Server) handleMCPRemove(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")
	res, err := s.Agent.MCPRemove(name)
	if err != nil {
		writeErr(w, 404, "%v", err)
		return
	}
	writeJSON(w, 200, res)
}

func (s *Server) handleWorkspaceGet(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, 200, s.Agent.WorkspaceView())
}

func (s *Server) handleWorkspaceSet(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Path string `json:"path"`
	}
	if err := readJSON(r, &body); err != nil || strings.TrimSpace(body.Path) == "" {
		writeErr(w, 400, "需要 path")
		return
	}
	view, err := s.Agent.WorkspaceSet(body.Path)
	if err != nil {
		writeErr(w, 400, "%v", err)
		return
	}
	writeJSON(w, 200, view)
}

func (s *Server) handleWorkspaceClear(w http.ResponseWriter, _ *http.Request) {
	view, err := s.Agent.WorkspaceClear()
	if err != nil {
		writeErr(w, 400, "%v", err)
		return
	}
	writeJSON(w, 200, view)
}

func (s *Server) handleFsBrowse(w http.ResponseWriter, r *http.Request) {
	res, err := s.Agent.WorkspaceBrowse(r.URL.Query().Get("path"))
	if err != nil {
		writeErr(w, 400, "%v", err)
		return
	}
	writeJSON(w, 200, res)
}

func (s *Server) handleToolPermission(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Name string `json:"name"`
		Perm string `json:"permission"`
	}
	if err := readJSON(r, &body); err != nil || body.Name == "" {
		writeErr(w, 400, "需要 name")
		return
	}
	res, err := s.Agent.ToolPermissionSet(body.Name, body.Perm)
	if err != nil {
		writeErr(w, 400, "%v", err)
		return
	}
	writeJSON(w, 200, res)
}

func (s *Server) handleGoStatus(w http.ResponseWriter, r *http.Request) {
	custom := r.URL.Query().Get("path")
	if custom != "" {
		st := DetectGo(custom)
		if !st.Found {
			st = GoStatus{Found: false, Source: "user-specified"}
		}
		writeJSON(w, 200, st)
		return
	}
	writeJSON(w, 200, s.managedGoStatus())
}

func (s *Server) handleGoStatusSet(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Path string `json:"path"`
	}
	if err := readJSON(r, &body); err != nil {
		writeErr(w, 400, "参数解析失败")
		return
	}
	st := DetectGo(body.Path)
	writeJSON(w, 200, st)
}

func (s *Server) handleOnboardingGet(w http.ResponseWriter, _ *http.Request) {
	needs := false
	if s.Agent.Cfg.LLM.Provider != "mock" {
		key, _ := agent.LLMKeyFor(s.Agent.Cfg, s.Agent.Creds, s.Agent.Cfg.LLM.BaseURL, "")
		needs = key == ""
	}
	writeJSON(w, 200, map[string]any{
		"needs_onboarding": needs,
		"providers":        s.Agent.ProvidersView(),
	})
}

func (s *Server) handleOnboardingSave(w http.ResponseWriter, r *http.Request) {
	var body struct {
		ProviderID string `json:"provider_id"`
		Plan       string `json:"plan"`
		APIKey     string `json:"api_key"`
		Mode       string `json:"mode"`
	}
	if err := readJSON(r, &body); err != nil {
		writeErr(w, 400, "参数解析失败")
		return
	}
	body.ProviderID = strings.TrimSpace(body.ProviderID)
	body.Plan = strings.TrimSpace(body.Plan)
	body.APIKey = strings.TrimSpace(body.APIKey)
	body.Mode = strings.TrimSpace(body.Mode)
	if body.ProviderID == "" {
		writeErr(w, 400, "请选择一个模型厂商")
		return
	}
	if body.APIKey == "" {
		writeErr(w, 400, "请填写 API Key")
		return
	}
	if body.Mode == "" {
		body.Mode = "auto"
	}

	patch := map[string]any{
		"llm": map[string]any{
			"provider_id": body.ProviderID,
			"plan":        body.Plan,
			"api_key":     body.APIKey,
			"base_url":    "",
			"model":       "",
		},
		"mode": body.Mode,
	}
	view, err := s.Agent.ApplySettings(patch)
	if err != nil {
		writeErr(w, 400, "%v", err)
		return
	}

	testResult := s.Agent.TestLLMConnection(map[string]any{
		"provider_id": body.ProviderID,
		"api_key":     body.APIKey,
	})

	writeJSON(w, 200, map[string]any{
		"saved":       true,
		"settings":    view,
		"test_result": testResult,
	})
}
