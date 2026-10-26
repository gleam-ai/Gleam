package agent

import (
	"context"
	"strings"
	"sync"
	"testing"
	"time"

	"gleam/internal/harness/conversation"
	"gleam/internal/harness/memory"
	"gleam/internal/llm"
	"gleam/pkg/types"
)

func TestWindowPct(t *testing.T) {
	cases := []struct{ last, window, want int }{
		{0, 128000, 0},
		{115200, 128000, 90},
		{115100, 128000, 90},
		{128000, 128000, 100},
		{200000, 128000, 100},
		{1000, 0, 0},
		{1000, 128000, 1},
	}
	for _, c := range cases {
		if got := windowPct(c.last, c.window); got != c.want {
			t.Errorf("windowPct(%d, %d) = %d，应为 %d", c.last, c.window, got, c.want)
		}
	}
}

func TestContextWindowResolution(t *testing.T) {
	f := newFixture(t, nil)

	f.a.Cfg.LLM.Model = "claude-sonnet-4"
	if got, src, _ := f.a.contextWindow(); got != 200000 || src != llm.SourceTable {
		t.Errorf("内置表：得到 %d/%s", got, src)
	}
	f.a.Cfg.LLM.Model = "谁也没见过的模型"
	if got, src, _ := f.a.contextWindow(); got != llm.DefaultContextWindow || src != llm.SourceDefault {
		t.Errorf("兜底：得到 %d/%s", got, src)
	}
	f.a.Cfg.LLM.ContextWindow = 32000
	if got, src, _ := f.a.contextWindow(); got != 32000 || src != llm.SourceManual {
		t.Errorf("手填应当压过内置表：得到 %d/%s", got, src)
	}
}

func TestCompressAfterTask(t *testing.T) {
	f := newFixture(t, nil)
	f.a.Cfg.LLM.ContextWindow = 1000
	ctx := context.Background()

	f.a.notePromptSize(llm.Usage{PromptTokens: 500})
	if c, tn := f.a.compressAfterTask(ctx, "t1", f.a.Mem.Generation()); c || tn {
		t.Errorf("水位 50%% 且无溢出时不该动上下文（得到 %v/%v）", c, tn)
	}
	if got := f.a.Mem.CarryTurns(); got != memory.CarryFull {
		t.Errorf("不该收紧，实际携带 %d 轮", got)
	}

	f.a.notePromptSize(llm.Usage{PromptTokens: 900})
	c, tn := f.a.compressAfterTask(ctx, "t1", f.a.Mem.Generation())
	if !tn {
		t.Error("水位 90% 应当收紧下一轮携带")
	}
	_ = c
	if got := f.a.Mem.CarryTurns(); got != memory.CarryTight {
		t.Errorf("应当收紧到 %d 轮，实际 %d", memory.CarryTight, got)
	}

	f2 := newFixture(t, nil)
	f2.a.Cfg.LLM.ContextWindow = 1000
	f2.a.notePromptSize(llm.Usage{PromptTokens: 100})
	for i := 0; i < f2.a.Cfg.Memory.ShortTermCap+3; i++ {
		f2.a.Mem.AddTurn("user", "第几轮不重要")
	}
	if f2.a.Mem.Stats().Overflow == 0 {
		t.Fatal("前提不成立：应当已经攒出溢出轮")
	}
	if c, _ := f2.a.compressAfterTask(ctx, "t2", f2.a.Mem.Generation()); !c {
		t.Error("有溢出轮就该汇总，与水位无关")
	}
}

func TestCompressMidTask(t *testing.T) {
	f := newFixture(t, nil)
	f.a.Cfg.LLM.ContextWindow = 1000
	ctx := context.Background()

	f.a.notePromptSize(llm.Usage{PromptTokens: 950})
	if _, tn := f.a.compressMidTask(ctx, "t1"); tn {
		t.Error("95% 还不该在任务中动手（96% 才是那条线）")
	}
	f.a.notePromptSize(llm.Usage{PromptTokens: 960})
	if _, tn := f.a.compressMidTask(ctx, "t1"); !tn {
		t.Error("96% 应当收紧")
	}
	if got := f.a.Mem.CarryTurns(); got != memory.CarryTight {
		t.Errorf("应当收紧到 %d 轮，实际 %d", memory.CarryTight, got)
	}
	f.a.notePromptSize(llm.Usage{PromptTokens: 400})
	f.a.compressMidTask(ctx, "t1")
	if got := f.a.Mem.CarryTurns(); got != memory.CarryFull {
		t.Errorf("水位掉回 40%% 应当松回 %d 轮，实际 %d", memory.CarryFull, got)
	}
}

func TestCompressionOffDoesNothing(t *testing.T) {
	f := newFixture(t, nil)
	f.a.Cfg.LLM.ContextWindow = 1000
	f.a.Cfg.Agent.ContextCompress = false
	f.a.notePromptSize(llm.Usage{PromptTokens: 999})
	ctx := context.Background()

	if c, tn := f.a.compressMidTask(ctx, "t1"); c || tn {
		t.Error("自动压缩关着时任务中不该动上下文")
	}
	if c, tn := f.a.compressAfterTask(ctx, "t1", f.a.Mem.Generation()); c || tn {
		t.Error("自动压缩关着时任务结束也不该动上下文")
	}
}

func TestContextViewCarriesWindowReading(t *testing.T) {
	f := newFixture(t, nil)
	f.a.Cfg.LLM.ContextWindow = 1000
	f.a.notePromptSize(llm.Usage{PromptTokens: 123, Estimated: true})

	v := f.a.ContextView()
	if v["window_tokens"] != 1000 || v["window_source"] != llm.SourceManual {
		t.Errorf("分母与来源不对：%v / %v", v["window_tokens"], v["window_source"])
	}
	if v["prompt_tokens"] != 123 || v["prompt_estimated"] != true {
		t.Errorf("读数应当是 123（估算）：%v / %v", v["prompt_tokens"], v["prompt_estimated"])
	}
	if v["window_pct"] != 13 {
		t.Errorf("占用应为 13%%，实际 %v", v["window_pct"])
	}
	if v["carry_turns"] != memory.CarryFull {
		t.Errorf("应当带上携带轮数，实际 %v", v["carry_turns"])
	}
	if v["compress_end_pct"] != compressAtEndPct || v["compress_mid_pct"] != compressMidPct {
		t.Errorf("阈值没带出去：%v / %v", v["compress_end_pct"], v["compress_mid_pct"])
	}
	if _, ok := v["fill_pct"]; !ok {
		t.Error("轮数口径不该被这次改动顶掉")
	}
}

func TestContextActionText(t *testing.T) {
	f := newFixture(t, nil)
	f.a.Cfg.LLM.ContextWindow = 1000
	f.a.notePromptSize(llm.Usage{PromptTokens: 970})
	f.a.Mem.SetCarryTurns(memory.CarryTight)

	got := f.a.contextActionText(true, true, true)
	for _, want := range []string{"97%", "1k", "2 轮最近对话", "任务继续"} {
		if !strings.Contains(got, want) {
			t.Errorf("这句话里应当有 %q：%s", want, got)
		}
	}
}

type compressionProbeNotifier struct {
	NopNotifier
	actions chan types.ProgressEvent
}

func (n *compressionProbeNotifier) OnProgress(ev types.ProgressEvent) {
	if ev.Phase == "done" || ev.Phase == "context" {
		n.actions <- ev
	}
}

func awaitCompressionProbe[T any](t *testing.T, ch <-chan T) T {
	t.Helper()
	select {
	case v := <-ch:
		return v
	case <-time.After(3 * time.Second):
		t.Fatal("compression probe timed out")
		var zero T
		return zero
	}
}

func TestAutoCompressionReproRollover(t *testing.T) {
	for _, scenario := range []string{"next_task", "switch_conversation"} {
		t.Run(scenario, func(t *testing.T) {
			a := lifecycleAgent(t)
			a.Cfg.Agent.ContextCompress = true
			a.Cfg.LLM.ContextWindow = 1000
			if scenario == "switch_conversation" {
				a.notePromptSize(llm.Usage{PromptTokens: 900})
			}
			n := &compressionProbeNotifier{actions: make(chan types.ProgressEvent, 4)}
			a.Notifier = n
			c, err := a.NewConversation()
			if err != nil {
				t.Fatal(err)
			}
			for i := 0; i < a.Mem.Stats().ShortCap-1; i++ {
				text := "recent history"
				if i == 0 {
					text = "A_EARLY_MARKER"
				}
				if _, err := a.Convos.Append(c.ID, conversation.Message{Role: "user", Content: text}); err != nil {
					t.Fatal(err)
				}
			}
			entered, release := make(chan struct{}), make(chan struct{})
			workerDone := make(chan (<-chan struct{}), 4)
			secondPrompt, releaseSecond := make(chan llm.ChatRequest, 1), make(chan struct{})
			var firstOnce, releaseOnce, secondOnce sync.Once
			unblock := func() { releaseOnce.Do(func() { close(release) }) }
			unblockSecond := func() { secondOnce.Do(func() { close(releaseSecond) }) }
			defer unblock()
			defer unblockSecond()
			a.LLM = &lifecycleClient{chat: func(ctx context.Context, r llm.ChatRequest) (string, error) {
				if strings.Contains(r.System, llm.MarkerCompress) {
					workerDone <- ctx.Done()
					firstOnce.Do(func() {
						close(entered)
						<-release
					})
					return "A_EARLY_MARKER summary", nil
				}
				if r.TaskID == "second" {
					secondPrompt <- r
					<-releaseSecond
				}
				return "answer", nil
			}}
			first := a.RunGoal(context.Background(), types.GoalRequest{
				TaskID: "first", Goal: "first question", TaskMode: types.TaskChat, ConversationID: c.ID,
			})
			if first.Status != types.GoalSuccess {
				t.Fatalf("first task failed: %+v", first)
			}
			awaitCompressionProbe(t, entered)
			if scenario == "switch_conversation" {
				if _, err := a.NewConversation(); err != nil {
					t.Fatal(err)
				}
				a.notePromptSize(llm.Usage{PromptTokens: 100})
				unblock()
				// finalize cancels this context only after the worker and its notification return.
				awaitCompressionProbe(t, awaitCompressionProbe(t, workerDone))
				if got := a.Mem.CarryTurns(); got != memory.CarryFull {
					t.Errorf("old task tightened new conversation: carry=%d want=%d", got, memory.CarryFull)
				}
				if got := a.Mem.Stats(); got.Summary != "" || got.Overflow != 0 || got.SavedTokens != 0 {
					t.Errorf("old compression contaminated new conversation: %+v", got)
				}
				select {
				case ev := <-n.actions:
					t.Errorf("old task emitted context action after switch: %+v", ev)
				default:
				}
				return
			}
			second, err := a.StartGoal(context.Background(), types.GoalRequest{
				TaskID: "second", Goal: "second question", TaskMode: types.TaskChat, ConversationID: c.ID,
			})
			if err != nil {
				t.Fatal(err)
			}
			r := awaitCompressionProbe(t, secondPrompt)
			visible := r.System
			for _, msg := range r.Messages {
				visible += "\n" + msg.Content
			}
			if !strings.Contains(visible, "A_EARLY_MARKER") {
				t.Error("next task prompt lost in-flight A_EARLY_MARKER")
			}
			unblock()
			awaitCompressionProbe(t, n.actions)
			awaitCompressionProbe(t, awaitCompressionProbe(t, workerDone))
			unblockSecond()
			if res := awaitCompressionProbe(t, second); res.Status != types.GoalSuccess {
				t.Fatalf("second task failed: %+v", res)
			}
			awaitCompressionProbe(t, n.actions)
			awaitCompressionProbe(t, awaitCompressionProbe(t, workerDone))
		})
	}
}

func TestAutoCompressionReproIdentity(t *testing.T) {
	t.Run("chat_task_id", func(t *testing.T) {
		a := lifecycleAgent(t)
		a.Cfg.Agent.ContextCompress = true
		n := &compressionProbeNotifier{actions: make(chan types.ProgressEvent, 4)}
		a.Notifier = n
		for i := 0; i < a.Mem.Stats().ShortCap; i++ {
			a.Mem.AddTurn("user", "early history")
		}
		requests := make(chan llm.ChatRequest, 4)
		workerDone := make(chan (<-chan struct{}), 1)
		a.LLM = &lifecycleClient{chat: func(ctx context.Context, r llm.ChatRequest) (string, error) {
			if strings.Contains(r.System, llm.MarkerCompress) {
				requests <- r
				if ctx.Done() != nil {
					workerDone <- ctx.Done()
				}
				return "history summary", nil
			}
			return "answer", nil
		}}
		a.runChatPath(context.Background(), "question", "chat-owner", "")
		for i := 0; i < 2; i++ {
			r := awaitCompressionProbe(t, requests)
			ev := awaitCompressionProbe(t, n.actions)
			if r.TaskID != "chat-owner" || ev.TaskID != "chat-owner" || ev.Phase != "context" {
				t.Errorf("chat compression taskID=%q event=%+v want=chat-owner/context", r.TaskID, ev)
			}
		}
		awaitCompressionProbe(t, awaitCompressionProbe(t, workerDone))
	})
	t.Run("finalize_model_and_feedback", func(t *testing.T) {
		a := lifecycleAgent(t)
		a.Cfg.Agent.ContextCompress = true
		n := &compressionProbeNotifier{actions: make(chan types.ProgressEvent, 4)}
		a.Notifier = n
		for i := 0; i <= a.Mem.Stats().ShortCap; i++ {
			a.Mem.AddTurn("user", "early history")
		}
		type call struct {
			client string
			owner  string
			err    error
			done   <-chan struct{}
		}
		calls := make(chan call, 2)
		client := func(name string) llm.Client {
			return &lifecycleClient{chat: func(ctx context.Context, r llm.ChatRequest) (string, error) {
				calls <- call{name, r.TaskID, ctx.Err(), ctx.Done()}
				return "history summary", nil
			}}
		}
		pinned := client("pinned")
		ctx, cancel := context.WithCancel(withTaskModels(context.Background(), taskModels{helper: pinned}))
		cancel()
		a.LLM = client("replacement")
		a.finalize(ctx, "original-task", &types.GoalResult{Status: types.GoalCancelled}, nil, "")
		got := awaitCompressionProbe(t, calls)
		ev := awaitCompressionProbe(t, n.actions)
		awaitCompressionProbe(t, got.done)
		if got.client != "pinned" || got.owner != "original-task" || got.err != nil {
			t.Errorf("background identity: client=%s taskID=%q err=%v", got.client, got.owner, got.err)
		}
		if ev.TaskID != "original-task" || ev.Phase != "context" {
			t.Errorf("compression feedback taskID=%q phase=%q want=original-task/context", ev.TaskID, ev.Phase)
		}
	})
}

func TestCompressionRejectsDelayedGeneration(t *testing.T) {
	f := newFixture(t, nil)
	f.a.Cfg.LLM.ContextWindow = 1000
	f.a.notePromptSize(llm.Usage{PromptTokens: 999})
	gen := f.a.Mem.Generation()
	f.a.Mem.SwitchConversation("new-conversation")
	for i := 0; i <= f.a.Mem.Stats().ShortCap; i++ {
		f.a.Mem.AddTurn("user", "NEW_CONVERSATION_MARKER")
	}
	called := false
	f.a.LLM = &lifecycleClient{chat: func(context.Context, llm.ChatRequest) (string, error) {
		called = true
		return "unexpected summary", nil
	}}
	if c, tn := f.a.compressAfterTask(context.Background(), "old-task", gen); c || tn || called {
		t.Fatalf("stale worker started on new context: compressed=%v tightened=%v model=%v", c, tn, called)
	}
	if got := f.a.Mem.Stats(); got.Overflow != 1 || got.Summary != "" || f.a.Mem.CarryTurns() != memory.CarryFull {
		t.Fatalf("stale worker modified new context: %+v", got)
	}
}

type blockedContextNotifier struct {
	NopNotifier
	entered chan struct{}
	release chan struct{}
}

func (n *blockedContextNotifier) OnProgress(ev types.ProgressEvent) {
	if ev.Phase == "context" && ev.TaskID == "previous" {
		close(n.entered)
		<-n.release
	}
}

func TestContextNotificationDoesNotBlockTaskAdmission(t *testing.T) {
	a := lifecycleAgent(t)
	a.Cfg.Agent.ContextCompress = false
	c, err := a.NewConversation()
	if err != nil {
		t.Fatal(err)
	}
	n := &blockedContextNotifier{entered: make(chan struct{}), release: make(chan struct{})}
	a.Notifier = n
	notified := make(chan struct{})
	go func() {
		a.notifyContextAction("previous", a.Mem.Generation(), true, false, false)
		close(notified)
	}()
	awaitCompressionProbe(t, n.entered)
	admitted := make(chan error, 1)
	finished := make(chan struct{})
	go func() {
		defer close(finished)
		done, err := a.StartGoal(context.Background(), types.GoalRequest{
			Goal: "next question", TaskMode: types.TaskChat, ConversationID: c.ID,
		})
		admitted <- err
		if err == nil {
			<-done
		}
	}()
	select {
	case err := <-admitted:
		if err != nil {
			t.Errorf("next task admission failed: %v", err)
		}
	case <-time.After(500 * time.Millisecond):
		t.Error("blocked compression notification prevented next task admission")
	}
	close(n.release)
	awaitCompressionProbe(t, notified)
	awaitCompressionProbe(t, finished)
}

func TestContextActionNotificationGeneration(t *testing.T) {
	f := newFixture(t, nil)
	n := &compressionProbeNotifier{actions: make(chan types.ProgressEvent, 4)}
	f.a.Notifier = n
	gen := f.a.Mem.Generation()
	f.a.notifyContextAction("original-task", gen, true, false, false)
	ev := awaitCompressionProbe(t, n.actions)
	if ev.TaskID != "original-task" || ev.Phase != "context" {
		t.Fatalf("wrong notification identity: %+v", ev)
	}
	f.a.Mem.SwitchConversation("new-conversation")
	f.a.notifyContextAction("original-task", gen, true, true, false)
	select {
	case ev := <-n.actions:
		t.Fatalf("stale notification reached new conversation: %+v", ev)
	default:
	}
}
