package main

import (
	"bufio"
	"strings"
	"testing"
	"time"

	"gleam/pkg/types"
)

func askWithStdin(t *testing.T, input string) types.ApprovalResponse {
	t.Helper()
	n := &consoleNotifier{stdin: bufio.NewReader(strings.NewReader(input)), approvalTimeout: time.Second}
	return n.OnApproval(types.ApprovalRequest{TaskID: "t", Plan: []string{"shell.exec: ls"}, Risk: "medium", Reason: "test"})
}

// A goal run from a script or with stdin closed has nobody to answer the
// prompt. That used to be recorded as "the user refused", which sent readers
// looking for a rejection that never happened.
func TestConsoleNotifier_ClosedStdinIsUnattended(t *testing.T) {
	resp := askWithStdin(t, "")
	if resp.Approved {
		t.Fatal("no answer must not approve")
	}
	if !resp.Unattended {
		t.Fatalf("closed stdin is an unattended denial: %+v", resp)
	}
	if resp.Note == "" {
		t.Error("the note should say why")
	}
}

func TestConsoleNotifier_TypedAnswerIsTheUsers(t *testing.T) {
	for _, in := range []string{"y\n", "Y\n", "yes\n", "y"} {
		if resp := askWithStdin(t, in); !resp.Approved {
			t.Errorf("input %q should approve: %+v", in, resp)
		}
	}
	for _, in := range []string{"n\n", "\n", "no\n"} {
		resp := askWithStdin(t, in)
		if resp.Approved || resp.Unattended {
			t.Errorf("input %q is a real refusal, got %+v", in, resp)
		}
	}
}
