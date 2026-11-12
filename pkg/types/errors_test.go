package types

import "testing"

func TestClassifyError(t *testing.T) {
	cases := []struct {
		msg  string
		want ErrorKind
	}{
		{"步骤超时（30s）: context deadline exceeded", ErrTimeout},
		{"too many requests: rate limited", ErrTimeout},
		{"permission denied: 403", ErrPermission},
		{"用户拒绝执行该操作（不想覆盖已有文件）", ErrPermission},
		{"file not found: notes.txt", ErrNotFound},
		{"引用的步骤 \"s2\" 未成功执行", ErrParam},
		{"missing required argument: path", ErrParam},
		{"命令以退出码 1 结束", ErrBusiness},
		{"502 bad gateway from upstream", ErrUpstream},
		{
			"目标站点返回了微信访问验证页，没有拿到正文。这是对方的风控策略，不是网络故障，重试同一 URL 不会成功。请用户完成验证或登录，或请用户直接粘贴正文",
			ErrNeedsHuman,
		},
		{"Cloudflare 人机验证页，需要人工接管", ErrNeedsHuman},
		{"boom", ErrUnknown},
	}
	for _, c := range cases {
		if got := ClassifyError(c.msg); got != c.want {
			t.Errorf("ClassifyError(%q) = %q，应为 %q", c.msg, got, c.want)
		}
	}
}

func TestContextBreakdownStateShare(t *testing.T) {
	var empty ContextBreakdown
	if empty.StateShare() != 0 {
		t.Error("空 breakdown 的占比应为 0")
	}
	b := ContextBreakdown{State: 600, Total: 1000}
	if b.StateShare() != 0.6 {
		t.Errorf("600/1000 应为 0.6，实际 %v", b.StateShare())
	}
}
