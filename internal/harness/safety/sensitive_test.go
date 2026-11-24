package safety

import (
	"path/filepath"
	"slices"
	"testing"
	"time"

	"gleam/pkg/types"
)

func TestGate_SensitiveFileNeedsApprovalInsideWorkspace(t *testing.T) {
	ws := t.TempDir()
	g := New("auto", nil, nil, []string{ws}, time.Minute)
	read := &fakeTool{name: "file.read", perm: types.PermissionReadOnly}

	for _, name := range []string{".env", ".env.local", "id_rsa", "server.pem", ".npmrc", ".ssh", filepath.Join(".ssh", "config")} {
		p := filepath.Join(ws, name)
		d := g.Evaluate(read, map[string]any{"path": p})
		if !d.NeedApproval {
			t.Fatalf("%s 在工作区内也应要求确认: %+v", name, d)
		}
		if !slices.Contains(d.Outside, p) {
			t.Errorf("%s 应进入审批路径列表: %+v", name, d.Outside)
		}
	}

	d := g.Evaluate(read, map[string]any{"path": filepath.Join(ws, "notes.md")})
	if d.NeedApproval {
		t.Errorf("普通文件不该被敏感清单误伤: %+v", d)
	}
}

func TestGate_TrustedReadToolStillAsksForSensitiveFile(t *testing.T) {
	ws := t.TempDir()
	g := New("auto", []string{"file.read"}, nil, []string{ws}, time.Minute)
	d := g.Evaluate(&fakeTool{name: "file.read", perm: types.PermissionReadOnly}, map[string]any{
		"path": filepath.Join(ws, ".env"),
	})
	if !d.NeedApproval {
		t.Errorf("信任 file.read 也不能静默读取 .env: %+v", d)
	}
}
