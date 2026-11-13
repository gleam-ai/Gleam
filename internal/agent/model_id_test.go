package agent

import "testing"

// Regression: auto IDs used UnixNano%100000 alone, so two adds in the same
// 100µs window produced duplicate IDs and the second add failed.
func TestModelAdd_RapidAddsGetUniqueIDs(t *testing.T) {
	a := newBindingAgent(t)
	seen := map[string]bool{}
	for i := 0; i < 30; i++ {
		out, err := a.ModelAdd(map[string]any{
			"name": "m", "base_url": "http://127.0.0.1:1", "model": "m", "protocol": "openai_chat",
		})
		if err != nil {
			t.Fatalf("add %d: %v", i, err)
		}
		id := out["model"].(map[string]any)["id"].(string)
		if seen[id] {
			t.Fatalf("duplicate model ID %q on add %d", id, i)
		}
		seen[id] = true
	}
}
