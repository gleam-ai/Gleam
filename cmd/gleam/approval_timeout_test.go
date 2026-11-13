package main

import (
	"testing"
	"time"
)

func TestApprovalTimeoutPrecedence(t *testing.T) {
	t.Run("config when no env", func(t *testing.T) {
		t.Setenv("GLEAM_APPROVAL_TIMEOUT_SEC", "")
		if got := approvalTimeout(30 * time.Second); got != 30*time.Second {
			t.Errorf("应使用配置值 30s，实际 %v", got)
		}
	})
	t.Run("env overrides config", func(t *testing.T) {
		t.Setenv("GLEAM_APPROVAL_TIMEOUT_SEC", "120")
		if got := approvalTimeout(30 * time.Second); got != 120*time.Second {
			t.Errorf("环境变量应覆盖配置，期望 120s，实际 %v", got)
		}
	})
	t.Run("default when unset", func(t *testing.T) {
		t.Setenv("GLEAM_APPROVAL_TIMEOUT_SEC", "")
		if got := approvalTimeout(0); got != 5*time.Minute {
			t.Errorf("未配置时应回退 5 分钟，实际 %v", got)
		}
	})
	t.Run("invalid env falls back to config", func(t *testing.T) {
		t.Setenv("GLEAM_APPROVAL_TIMEOUT_SEC", "abc")
		if got := approvalTimeout(45 * time.Second); got != 45*time.Second {
			t.Errorf("非法环境变量应回退配置值 45s，实际 %v", got)
		}
	})
}
