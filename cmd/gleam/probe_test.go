package main

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestProbeAddr_RecognizesGuardedGleam(t *testing.T) {
	guarded := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("X-Gleam-Server", "gleam")
		http.Error(w, `{"error":"缺少或错误的 API 口令"}`, http.StatusUnauthorized)
	}))
	defer guarded.Close()
	if busy, isGleam := probeAddr(strings.TrimPrefix(guarded.URL, "http://")); !busy || !isGleam {
		t.Errorf("带守卫的 Gleam: busy=%v isGleam=%v", busy, isGleam)
	}

	legacy := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"name":"gleam","version":"1.0.1"}`))
	}))
	defer legacy.Close()
	if busy, isGleam := probeAddr(strings.TrimPrefix(legacy.URL, "http://")); !busy || !isGleam {
		t.Errorf("旧版 Gleam: busy=%v isGleam=%v", busy, isGleam)
	}

	other := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "nope", http.StatusUnauthorized)
	}))
	defer other.Close()
	if busy, isGleam := probeAddr(strings.TrimPrefix(other.URL, "http://")); !busy || isGleam {
		t.Errorf("别的服务: busy=%v isGleam=%v", busy, isGleam)
	}
}
