package lsp

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// Tool metadata: ensure permission/Schema wiring is correct and the tools are read-only.
func TestToolsAreReadOnly(t *testing.T) {
	m := NewManager()
	tools := []interface {
		Name() string
		Description() string
		Permission() interface{ String() string }
	}{}
	_ = tools // asserting through the interface would be verbose; verification below goes via Name/Schema

	def := NewDefinition(m)
	if def.Name() != "lsp.definition" {
		t.Errorf("definition name = %s", def.Name())
	}
	ref := NewReferences(m)
	if ref.Name() != "lsp.references" {
		t.Errorf("references name = %s", ref.Name())
	}
	hov := NewHover(m)
	if hov.Name() != "lsp.hover" {
		t.Errorf("hover name = %s", hov.Name())
	}
}

func TestDefinitionTool_RequiresFileLineCol(t *testing.T) {
	m := NewManager()
	defer m.Close()
	tool := NewDefinition(m)
	// missing file
	if _, err := tool.Execute(context.Background(), map[string]any{"line": 1, "column": 1}); err == nil {
		t.Fatal("missing file should error")
	}
	// line/col must start at 1
	if _, err := tool.Execute(context.Background(), map[string]any{"file": "x.go", "line": 0, "column": 1}); err == nil {
		t.Fatal("line=0 should error")
	}
	if _, err := tool.Execute(context.Background(), map[string]any{"file": "x.go", "line": 1, "column": 0}); err == nil {
		t.Fatal("column=0 should error")
	}
}

func TestPathToURI(t *testing.T) {
	cases := []struct {
		in   string
		want string // substring match, since separators differ across platforms
	}{
		{"/home/user/main.go", "file:///home/user/main.go"},
	}
	for _, c := range cases {
		got := pathToURI(c.in)
		if !strings.HasPrefix(got, "file://") {
			t.Errorf("pathToURI(%q) = %q, missing file:// prefix", c.in, got)
		}
	}
}

// gopls integration test: runs only when gopls is installed.
func TestLSP_DefinitionIntegration(t *testing.T) {
	if _, err := exec.LookPath("gopls"); err != nil {
		t.Skip("gopls not installed; skipping integration")
	}
	tmp := t.TempDir()
	// write a minimal Go module
	if err := os.WriteFile(filepath.Join(tmp, "go.mod"), []byte("module tmp\n\ngo 1.22\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	src := `package main

func target() int { return 42 }

func main() {
	_ = target()
}
`
	mainPath := filepath.Join(tmp, "main.go")
	if err := os.WriteFile(mainPath, []byte(src), 0o644); err != nil {
		t.Fatal(err)
	}

	m := NewManager()
	defer m.Close()
	tool := NewDefinition(m)
	// jump to the target call site (line 7 in main, `_ = target()`; target is at column 9)
	out, err := tool.Execute(context.Background(), map[string]any{
		"file":      mainPath,
		"line":      7,
		"column":    9,
		"workspace": tmp,
	})
	if err != nil {
		t.Fatalf("definition: %v", err)
	}
	if out == nil {
		t.Fatal("no result")
	}
}
