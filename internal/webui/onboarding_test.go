package webui

import "testing"

// The wizard has no model picker; it used to POST model="", which ApplySettings
// rejects, leaving a fresh install with no usable model. The handler now fills
// the provider preset's first model.
func TestOnboardingModelFillsProviderPreset(t *testing.T) {
	if got := onboardingModel("deepseek"); got != "deepseek-v4-pro" {
		t.Errorf("deepseek onboarding model = %q", got)
	}
	if got := onboardingModel("not-a-provider"); got != "" {
		t.Errorf("unknown provider should stay empty, got %q", got)
	}
}
