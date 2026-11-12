package server

import (
	"context"
	"encoding/json"
	"net"
	"testing"
	"time"
)

func decodeWithin(t *testing.T, dec *json.Decoder) incoming {
	t.Helper()
	ch := make(chan incoming, 1)
	go func() {
		var msg incoming
		if err := dec.Decode(&msg); err == nil {
			ch <- msg
		}
	}()
	select {
	case msg := <-ch:
		return msg
	case <-time.After(3 * time.Second):
		t.Fatal("no RPC response within deadline")
		return incoming{}
	}
}

func TestDispatchPanicBecomesRPCError(t *testing.T) {
	c1, c2 := net.Pipe()
	defer c1.Close()
	defer c2.Close()
	srv := NewConn(c2, c2)
	srv.Handle("boom", func(context.Context, json.RawMessage) (any, *RPCError) {
		panic("kaboom")
	})
	srv.Handle("ok", func(context.Context, json.RawMessage) (any, *RPCError) {
		return "fine", nil
	})
	go srv.Serve()

	enc := json.NewEncoder(c1)
	dec := json.NewDecoder(c1)
	if err := enc.Encode(map[string]any{"jsonrpc": "2.0", "id": 1, "method": "boom"}); err != nil {
		t.Fatal(err)
	}
	resp := decodeWithin(t, dec)
	if resp.Error == nil {
		t.Fatal("handler panic 应转成 RPC 错误而不是进程崩溃")
	}
	if err := enc.Encode(map[string]any{"jsonrpc": "2.0", "id": 2, "method": "ok"}); err != nil {
		t.Fatal(err)
	}
	resp = decodeWithin(t, dec)
	if resp.Error != nil {
		t.Fatalf("panic 之后连接应继续服务: %v", resp.Error)
	}
}

func TestDispatchCtxCancelledOnClose(t *testing.T) {
	c1, c2 := net.Pipe()
	defer c2.Close()
	srv := NewConn(c2, c2)
	entered := make(chan struct{})
	exited := make(chan struct{})
	srv.Handle("block", func(ctx context.Context, _ json.RawMessage) (any, *RPCError) {
		close(entered)
		<-ctx.Done()
		close(exited)
		return nil, Errf(CodeInternal, "cancelled")
	})
	go srv.Serve()
	go func() {
		_ = json.NewEncoder(c1).Encode(map[string]any{"jsonrpc": "2.0", "id": 1, "method": "block"})
	}()
	select {
	case <-entered:
	case <-time.After(3 * time.Second):
		t.Fatal("handler not entered")
	}
	c1.Close()
	select {
	case <-exited:
	case <-time.After(3 * time.Second):
		t.Fatal("连接关闭后 handler 的 ctx 应被取消")
	}
}
