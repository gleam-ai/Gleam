package main

import (
	"os"
	"path/filepath"
	"testing"

	"gleam/internal/agent"
	"gleam/internal/config"
)

// TestBuildRuntimeLayerPrecedence locks the config layering contract:
// config.yaml < settings.yaml (overlay) < GLEAM_* env < CLI flags. Each layer
// sets workspace to a distinct directory; the highest layer present must win.
// Regression for qwen4-eval #9: LoadOverlay overwrites whatever the lower
// layers set, so buildRuntime must replay ApplyEnv and the CLI flags after it
// -- otherwise a workspace in settings.yaml silently beats GLEAM_WORKSPACE
// and --workspace.
func TestBuildRuntimeLayerPrecedence(t *testing.T) {
	root := t.TempDir()
	mk := func(name string) string {
		p := filepath.Join(root, name)
		if err := os.MkdirAll(p, 0o755); err != nil {
			t.Fatal(err)
		}
		return p
	}
	cfgWS := mk("cfg-ws")
	overlayWS := mk("overlay-ws")
	envWS := mk("env-ws")
	cliWS := mk("cli-ws")
	dataDir := mk("data")

	writeYAML := func(path, ws string) {
		b, err := config.MarshalYAML(config.NewYMap().Set("workspace", ws))
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, b, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	configPath := filepath.Join(root, "config.yaml")
	writeYAML(configPath, cfgWS)
	overlayPath := filepath.Join(dataDir, config.OverlayFile)

	build := func(t *testing.T, cli string) *config.Config {
		t.Helper()
		rt, err := buildRuntime(configPath, cli, dataDir, true, "", agent.NopNotifier{})
		if err != nil {
			t.Fatalf("装配 runtime 失败: %v", err)
		}
		t.Cleanup(rt.cleanup)
		return rt.cfg
	}

	for _, tc := range []struct {
		name    string
		cli     string
		env     string
		overlay bool
		want    string
	}{
		{"cli beats env overlay config", cliWS, envWS, true, cliWS},
		{"env beats overlay config", "", envWS, true, envWS},
		{"overlay beats config", "", "", true, overlayWS},
		{"config only", "", "", false, cfgWS},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv("GLEAM_WORKSPACE", tc.env)
			if tc.overlay {
				writeYAML(overlayPath, overlayWS)
			} else if err := os.Remove(overlayPath); err != nil && !os.IsNotExist(err) {
				t.Fatal(err)
			}
			if got := build(t, tc.cli).Workspace; got != tc.want {
				t.Errorf("workspace = %q, want %q（层优先级被破坏）", got, tc.want)
			}
		})
	}
}
