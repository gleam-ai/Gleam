package agent

import (
	"context"
	"errors"
	"strings"
	"testing"

	"gleam/internal/harness/registry"
	"gleam/internal/harness/safety"
	"gleam/internal/llm"
	"gleam/pkg/types"
)

type fakeComposer struct {
	calls  int
	goal   string
	drafts []string
	out    string
	err    error
}

func (f *fakeComposer) Compose(_ context.Context, goal, draft string) (string, error) {
	f.calls++
	f.goal = goal
	f.drafts = append(f.drafts, draft)
	return f.out, f.err
}

func newComposerExecutor(composer ReplyComposer) (*Executor, *registry.Registry) {
	r := registry.New()
	r.MustRegister(&funcTool{name: "producer", perm: types.PermissionReadOnly, fn: func(ctx context.Context, args map[string]any) (any, error) {
		return map[string]any{"text": "PRODUCED"}, nil
	}})
	r.MustRegister(&funcTool{name: "reply", perm: types.PermissionReadOnly, fn: func(ctx context.Context, args map[string]any) (any, error) {
		return map[string]any{"text": args["text"]}, nil
	}})
	e := &Executor{
		Reg:      r,
		Gate:     safety.New("auto", nil, nil, nil, 0),
		Goal:     "the goal",
		Composer: composer,
	}
	return e, r
}

// A reply that cites an earlier step is written by the model from what that
// step returned, not echoed from the draft the planner wrote before it ran.
func TestExecute_ReplyComposedFromEvidence(t *testing.T) {
	composer := &fakeComposer{out: "  final answer\n"}
	e, _ := newComposerExecutor(composer)
	plan := types.Plan{Steps: []types.Step{
		{ID: "s1", Tool: "producer"},
		{ID: "s2", Tool: "reply", DependsOn: []string{"s1"}, Args: map[string]any{
			"text": "draft {ref:s1.text}",
		}},
	}}
	res := e.Execute(context.Background(), plan, "t", "auto", false)
	if res.Succeeded != 2 {
		t.Fatalf("succeeded = %d, s2 error: %s", res.Succeeded, res.ByID["s2"].Error)
	}
	if composer.calls != 1 {
		t.Fatalf("compose calls = %d, want 1", composer.calls)
	}
	if composer.goal != "the goal" {
		t.Errorf("goal passed to composer = %q", composer.goal)
	}
	if len(composer.drafts) != 1 || composer.drafts[0] != "draft PRODUCED" {
		t.Errorf("draft = %q, want the interpolated one", composer.drafts)
	}
	if res.ReplyText != "final answer" {
		t.Errorf("reply text = %q, want the composed answer", res.ReplyText)
	}
	if got := res.ByID["s2"].FinalArgs["text"]; got != "final answer" {
		t.Errorf("FinalArgs text = %v, want the composed answer", got)
	}
}

// The planner may cite a step without listing it in depends_on; the reference
// alone marks the text as a draft.
func TestExecute_ReplyReferencingWithoutDependsOnIsComposed(t *testing.T) {
	composer := &fakeComposer{out: "final answer"}
	e, _ := newComposerExecutor(composer)
	plan := types.Plan{Steps: []types.Step{
		{ID: "s1", Tool: "producer"},
		{ID: "s2", Tool: "reply", Args: map[string]any{"text": "{ref:s1.text}"}},
	}}
	res := e.Execute(context.Background(), plan, "t", "auto", false)
	if res.Succeeded != 2 {
		t.Fatalf("succeeded = %d, s2 error: %s", res.Succeeded, res.ByID["s2"].Error)
	}
	if composer.calls != 1 || res.ReplyText != "final answer" {
		t.Errorf("calls = %d reply = %q, want 1 and the composed answer", composer.calls, res.ReplyText)
	}
}

// A standalone reply is already the whole answer; rewriting it costs a call and
// can only make it worse.
func TestExecute_StandaloneReplyIsNotComposed(t *testing.T) {
	composer := &fakeComposer{out: "rewritten"}
	e, _ := newComposerExecutor(composer)
	plan := types.Plan{Steps: []types.Step{
		{ID: "s1", Tool: "reply", Args: map[string]any{"text": "hello"}},
	}}
	res := e.Execute(context.Background(), plan, "t", "auto", false)
	if composer.calls != 0 {
		t.Fatalf("compose calls = %d, want 0", composer.calls)
	}
	if res.ReplyText != "hello" {
		t.Errorf("reply text = %q, want the planned text", res.ReplyText)
	}
}

func TestExecute_ReplyComposerFailureKeepsDraft(t *testing.T) {
	for name, composer := range map[string]*fakeComposer{
		"error":   {err: errors.New("model down")},
		"empty":   {out: "   "},
		"nodraft": {},
	} {
		t.Run(name, func(t *testing.T) {
			draft := "draft {ref:s1.text}"
			if name == "nodraft" {
				draft = "   "
			}
			e, _ := newComposerExecutor(composer)
			plan := types.Plan{Steps: []types.Step{
				{ID: "s1", Tool: "producer"},
				{ID: "s2", Tool: "reply", DependsOn: []string{"s1"}, Args: map[string]any{"text": draft}},
			}}
			res := e.Execute(context.Background(), plan, "t", "auto", false)
			if res.Succeeded != 2 {
				t.Fatalf("succeeded = %d, s2 error: %s", res.Succeeded, res.ByID["s2"].Error)
			}
			want := "draft PRODUCED"
			if name == "nodraft" {
				// Nothing to rewrite: no call, and the blank text is kept.
				want = "   "
				if composer.calls != 0 {
					t.Errorf("compose calls = %d, want 0 for a blank draft", composer.calls)
				}
			}
			if res.ReplyText != want {
				t.Errorf("reply text = %q, want the draft %q", res.ReplyText, want)
			}
		})
	}
}

func TestExecute_NilComposerKeepsPlannedText(t *testing.T) {
	e, _ := newComposerExecutor(nil)
	plan := types.Plan{Steps: []types.Step{
		{ID: "s1", Tool: "producer"},
		{ID: "s2", Tool: "reply", DependsOn: []string{"s1"}, Args: map[string]any{"text": "draft {ref:s1.text}"}},
	}}
	res := e.Execute(context.Background(), plan, "t", "auto", false)
	if res.ReplyText != "draft PRODUCED" {
		t.Errorf("reply text = %q, want the interpolated draft", res.ReplyText)
	}
}

type composerClient struct {
	req  llm.ChatRequest
	text string
	err  error
}

func (c *composerClient) Chat(_ context.Context, req llm.ChatRequest) (string, error) {
	c.req = req
	return c.text, c.err
}
func (c *composerClient) ChatStream(ctx context.Context, req llm.ChatRequest, _ func(string)) (string, error) {
	return c.Chat(ctx, req)
}
func (c *composerClient) Name() string { return "composer-client" }

func TestNewReplyComposer(t *testing.T) {
	if NewReplyComposer(nil, "t") != nil {
		t.Error("nil client must yield a nil composer")
	}
	if NewReplyComposer(llm.NewMock(), "t") != nil {
		t.Error("the offline mock must yield a nil composer")
	}
	if NewReplyComposer(&composerClient{}, "t") == nil {
		t.Error("a real client must yield a composer")
	}
}

func TestLLMReplyComposerCompose(t *testing.T) {
	client := &composerClient{text: "  answer \n"}
	c := NewReplyComposer(client, "task-1")
	if c == nil {
		t.Fatal("composer is nil")
	}
	got, err := c.Compose(context.Background(), "my goal", "my draft")
	if err != nil {
		t.Fatalf("compose: %v", err)
	}
	if got != "answer" {
		t.Errorf("composed = %q, want trimmed answer", got)
	}
	req := client.req
	if kind := llm.KindOf(req.System); kind != "compose" {
		t.Errorf("task kind = %q, want compose", kind)
	}
	if len(req.Messages) != 1 || req.Messages[0].Role != llm.RoleUser {
		t.Fatalf("messages = %+v, want one user message", req.Messages)
	}
	body := req.Messages[0].Content
	if !strings.Contains(body, "my goal") || !strings.Contains(body, "my draft") {
		t.Errorf("message must carry goal and draft, got %q", body)
	}
	if req.TaskID != "task-1" {
		t.Errorf("task id = %q", req.TaskID)
	}

	client.err = errors.New("down")
	if _, err := c.Compose(context.Background(), "g", "d"); err == nil {
		t.Error("a failed call must surface its error")
	}
}
