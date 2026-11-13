package webui

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestGuard_TokenFilePerInstanceOwnership proves two sidecars sharing a data dir
// do not clobber each other's API token: the first owns canonical webui.token,
// the second writes a per-instance file, and ownership is reclaimable after the
// first releases it.
func TestGuard_TokenFilePerInstanceOwnership(t *testing.T) {
	f := newFixture(t, nil)
	canonical := filepath.Join(f.dataDir, TokenFileName)

	f.srv.BindAddr = "127.0.0.1:8001"
	p1, err := f.srv.WriteTokenFile()
	if err != nil {
		t.Fatal(err)
	}
	if p1 != canonical {
		t.Fatalf("first instance should own canonical, got %s", p1)
	}
	if b, _ := os.ReadFile(canonical); strings.TrimSpace(string(b)) != f.srv.Token() {
		t.Fatal("canonical content != first token")
	}

	s2 := NewServer(f.agent)
	s2.BindAddr = "127.0.0.1:8002"
	p2, err := s2.WriteTokenFile()
	if err != nil {
		t.Fatal(err)
	}
	if p2 == canonical {
		t.Fatalf("second instance clobbered canonical: %s", p2)
	}
	if want := filepath.Join(f.dataDir, "webui.8002.token"); p2 != want {
		t.Errorf("second instance file = %s, want %s", p2, want)
	}
	if b, _ := os.ReadFile(p2); strings.TrimSpace(string(b)) != s2.Token() {
		t.Error("per-instance content != second token")
	}
	if b, _ := os.ReadFile(canonical); strings.TrimSpace(string(b)) != f.srv.Token() {
		t.Error("canonical was overwritten by the second instance")
	}
	s2.ReleaseTokenFile()

	f.srv.ReleaseTokenFile()
	s3 := NewServer(f.agent)
	s3.BindAddr = "127.0.0.1:8003"
	p3, err := s3.WriteTokenFile()
	if err != nil {
		t.Fatal(err)
	}
	if p3 != canonical {
		t.Errorf("after release, new instance should reclaim canonical, got %s", p3)
	}
	if b, _ := os.ReadFile(canonical); strings.TrimSpace(string(b)) != s3.Token() {
		t.Error("canonical content != third token after reclaim")
	}
	s3.ReleaseTokenFile()
}
