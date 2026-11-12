package agent

import (
	"fmt"
	"runtime"
	"sync"
	"testing"

	"gleam/internal/config"
)

// TestConfigSnapshot_ConsistentUnderHotReload is the regression guard for the
// settings hot-reload race (#68): a reader that captures one Config() pointer
// must see a single coherent generation across every field, even while writers
// swap the config concurrently. The old in-place mutation let a reader observe
// Model from one save and MaxSteps from the next (a torn config). Copy-on-write
// makes each published snapshot immutable, so the three tagged fields always
// agree. Run with -race to also catch the underlying data race.
func TestConfigSnapshot_ConsistentUnderHotReload(t *testing.T) {
	a := &Agent{Cfg: config.Default()}
	// Seed a coherent generation 0 so readers never see the unrelated defaults.
	a.Cfg.LLM.Model = "model-0"
	a.Cfg.Agent.MaxSteps = 0
	a.Cfg.DataDir = "/data-0"

	const writers, readers, iters = 4, 4, 3000
	var wg sync.WaitGroup

	for w := 0; w < writers; w++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := 1; i <= iters; i++ {
				gen := i
				_ = a.updateConfig(func(cfg *config.Config) error {
					cfg.LLM.Model = fmt.Sprintf("model-%d", gen)
					cfg.Agent.MaxSteps = gen
					cfg.DataDir = fmt.Sprintf("/data-%d", gen)
					return nil
				})
				if i%64 == 0 {
					runtime.Gosched()
				}
			}
		}()
	}

	for r := 0; r < readers; r++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := 0; i < iters; i++ {
				cfg := a.Config() // one snapshot, read lock-free below
				gen := cfg.Agent.MaxSteps
				if cfg.LLM.Model != fmt.Sprintf("model-%d", gen) || cfg.DataDir != fmt.Sprintf("/data-%d", gen) {
					t.Errorf("torn config snapshot: gen=%d model=%q dataDir=%q", gen, cfg.LLM.Model, cfg.DataDir)
					return
				}
				if i%64 == 0 {
					runtime.Gosched()
				}
			}
		}()
	}
	wg.Wait()
}

// TestUpdateConfig_RejectedSavePublishesNothing verifies the atomicity contract:
// when the callback returns an error the published config must be the exact
// same pointer with none of the callback's mutations applied.
func TestUpdateConfig_RejectedSavePublishesNothing(t *testing.T) {
	a := &Agent{Cfg: config.Default()}
	a.Cfg.LLM.Model = "before"
	before := a.Config()

	err := a.updateConfig(func(cfg *config.Config) error {
		cfg.LLM.Model = "after"
		cfg.Agent.MaxSteps = 999
		return fmt.Errorf("save rejected")
	})
	if err == nil {
		t.Fatal("expected the callback error to propagate")
	}
	after := a.Config()
	if after != before {
		t.Error("a rejected save must not swap the published pointer")
	}
	if after.LLM.Model != "before" || after.Agent.MaxSteps == 999 {
		t.Errorf("rejected save leaked mutations: model=%q maxSteps=%d", after.LLM.Model, after.Agent.MaxSteps)
	}
}

func TestUpdateConfig_NilConfig(t *testing.T) {
	a := &Agent{}
	if err := a.updateConfig(func(*config.Config) error { return nil }); err == nil {
		t.Error("updateConfig on a nil config should error, not panic")
	}
}

// TestModelSwitch_NoDeadlockUnderConfigLock guards the reentrancy rule: the
// model-switch handlers build their response view (modelEntryView -> llmKeyFor
// -> Config()) which takes cfgMu.RLock. Doing that inside the updateConfig
// callback deadlocks against the held write lock. The view must be built after
// publish. Without the fix this test hangs until the package timeout.
func TestModelSwitch_NoDeadlockUnderConfigLock(t *testing.T) {
	a := &Agent{Cfg: config.Default()}
	a.Cfg.DataDir = t.TempDir()
	a.Cfg.LLM.Provider = "custom"
	a.Cfg.LLM.Models = []config.ModelEntry{
		{ID: "m1", Name: "one", Model: "model-1", BaseURL: "https://a.test/v1", IsDefault: true},
		{ID: "m2", Name: "two", Model: "model-2", BaseURL: "https://b.test/v1"},
	}

	if _, err := a.SetActiveModel("m2"); err != nil {
		t.Fatalf("SetActiveModel: %v", err)
	}
	if _, err := a.ModelSetDefault("m1"); err != nil {
		t.Fatalf("ModelSetDefault: %v", err)
	}
	cfg := a.Config()
	if !cfg.LLM.Models[0].IsDefault || cfg.LLM.Models[1].IsDefault {
		t.Errorf("default flag should rest on m1: %+v", cfg.LLM.Models)
	}
}
