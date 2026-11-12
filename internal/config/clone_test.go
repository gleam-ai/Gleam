package config

import "testing"

// TestClone_DeepCopiesReferenceFields guards the copy-on-write contract: a
// published Config is read without locks, so updateConfig must hand writers a
// clone that shares no mutable backing array or map with the original. If any
// nested slice/map were aliased, mutating the clone would tear the config a
// running task is still reading.
func TestClone_DeepCopiesReferenceFields(t *testing.T) {
	orig := Default()
	orig.LLM.Tiers = map[string]string{"fast": "m-fast"}
	orig.LLM.Models = []ModelEntry{{ID: "a", Model: "m-a", APIKey: "k"}}
	orig.Safety.TrustedPaths = []string{"/trusted"}
	orig.Safety.TrustedTools = []string{"file.read"}
	orig.Safety.ToolPermissions = map[string]string{"file.write": "ask"}
	orig.Safety.Hooks = []HookConfig{{Event: "pre_tool", Tools: []string{"shell.exec"}, Command: "echo"}}
	orig.WorkspaceRecents = []string{"/ws"}
	orig.MCP = []MCPServerConfig{{
		Name: "srv", Args: []string{"--flag"},
		Env: map[string]string{"E": "1"}, Headers: map[string]string{"H": "2"},
	}}

	cl := orig.Clone()

	// Mutate every nested reference field on the clone.
	cl.LLM.Tiers["fast"] = "changed"
	cl.LLM.Tiers["new"] = "added"
	cl.LLM.Models[0].Model = "changed"
	cl.LLM.Models = append(cl.LLM.Models, ModelEntry{ID: "b"})
	cl.Safety.TrustedPaths[0] = "changed"
	cl.Safety.TrustedTools[0] = "changed"
	cl.Safety.ToolPermissions["file.write"] = "changed"
	cl.Safety.Hooks[0].Tools[0] = "changed"
	cl.Safety.Hooks[0].Command = "changed"
	cl.WorkspaceRecents[0] = "changed"
	cl.MCP[0].Args[0] = "changed"
	cl.MCP[0].Env["E"] = "changed"
	cl.MCP[0].Headers["H"] = "changed"
	cl.MCP[0].Name = "changed"
	cl.DataDir = "changed"
	cl.Workspace = "changed"

	// The original must be untouched in every aliased field.
	if orig.LLM.Tiers["fast"] != "m-fast" || len(orig.LLM.Tiers) != 1 {
		t.Errorf("LLM.Tiers aliased: %v", orig.LLM.Tiers)
	}
	if orig.LLM.Models[0].Model != "m-a" || len(orig.LLM.Models) != 1 {
		t.Errorf("LLM.Models aliased: %+v", orig.LLM.Models)
	}
	if orig.Safety.TrustedPaths[0] != "/trusted" {
		t.Errorf("TrustedPaths aliased: %v", orig.Safety.TrustedPaths)
	}
	if orig.Safety.TrustedTools[0] != "file.read" {
		t.Errorf("TrustedTools aliased: %v", orig.Safety.TrustedTools)
	}
	if orig.Safety.ToolPermissions["file.write"] != "ask" {
		t.Errorf("ToolPermissions aliased: %v", orig.Safety.ToolPermissions)
	}
	if orig.Safety.Hooks[0].Tools[0] != "shell.exec" || orig.Safety.Hooks[0].Command != "echo" {
		t.Errorf("Hooks aliased: %+v", orig.Safety.Hooks)
	}
	if orig.WorkspaceRecents[0] != "/ws" {
		t.Errorf("WorkspaceRecents aliased: %v", orig.WorkspaceRecents)
	}
	if orig.MCP[0].Args[0] != "--flag" || orig.MCP[0].Env["E"] != "1" || orig.MCP[0].Headers["H"] != "2" || orig.MCP[0].Name != "srv" {
		t.Errorf("MCP aliased: %+v", orig.MCP[0])
	}
	if orig.DataDir == "changed" || orig.Workspace == "changed" {
		t.Errorf("scalar fields aliased: DataDir=%q Workspace=%q", orig.DataDir, orig.Workspace)
	}
}

func TestClone_NilAndEmpty(t *testing.T) {
	var nilCfg *Config
	if nilCfg.Clone() != nil {
		t.Error("nil.Clone() should be nil")
	}
	// An empty config must clone without panicking on nil maps/slices.
	cl := (&Config{}).Clone()
	if cl == nil {
		t.Fatal("empty.Clone() should not be nil")
	}
	if cl.LLM.Tiers != nil || cl.MCP != nil || cl.Safety.Hooks != nil {
		t.Error("nil nested fields should stay nil after clone")
	}
}
