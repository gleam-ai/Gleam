package webui

import (
	"context"
	"testing"
	"time"

	"gleam/pkg/types"
)

func waitApprovalID(t *testing.T, s *Server) string {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		s.mu.Lock()
		var id string
		for k := range s.approvals {
			id = k
		}
		s.mu.Unlock()
		if id != "" {
			return id
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatal("approval not registered")
	return ""
}

func TestOnApproval_ResolveOwnership(t *testing.T) {
	s := NewServer(newTestAgent(t))
	respCh := make(chan types.ApprovalResponse, 1)
	go func() {
		respCh <- s.onApprovalContext(context.Background(), types.ApprovalRequest{TaskID: "t"})
	}()
	id := waitApprovalID(t, s)
	if !s.resolve(id, types.ApprovalResponse{Approved: true, Note: "user"}) {
		t.Fatal("first resolve should win")
	}
	if s.resolve(id, types.ApprovalResponse{Approved: false, Note: "timeout"}) {
		t.Error("second resolve must lose")
	}
	select {
	case resp := <-respCh:
		if !resp.Approved {
			t.Errorf("先到的用户决定应被采纳: %+v", resp)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("onApprovalContext did not return")
	}
}

func TestOnApproval_CancelRetiresCard(t *testing.T) {
	s := NewServer(newTestAgent(t))
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	respCh := make(chan types.ApprovalResponse, 1)
	go func() {
		respCh <- s.onApprovalContext(ctx, types.ApprovalRequest{TaskID: "t"})
	}()
	waitApprovalID(t, s)
	cancel()
	select {
	case resp := <-respCh:
		if resp.Approved {
			t.Errorf("取消的审批必须拒绝: %+v", resp)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("onApprovalContext did not return on cancel")
	}
	s.mu.Lock()
	n := len(s.approvals)
	s.mu.Unlock()
	if n != 0 {
		t.Errorf("取消后仍残留 %d 个审批", n)
	}
}
