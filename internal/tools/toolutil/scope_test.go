package toolutil

import (
	"context"
	"testing"
)

func TestRootsFromFallsBack(t *testing.T) {
	fallback := []string{`D:\ws`}

	if got := RootsFrom(context.Background(), fallback); len(got) != 1 || got[0] != fallback[0] {
		t.Errorf("ctx 里没边界时应当回落，实际 %v", got)
	}
	if got := RootsFrom(nil, fallback); len(got) != 1 || got[0] != fallback[0] {
		t.Errorf("nil ctx 也应当回落，实际 %v", got)
	}
	ctx := WithRoots(context.Background(), nil)
	if got := RootsFrom(ctx, fallback); len(got) != 1 || got[0] != fallback[0] {
		t.Errorf("空 roots 应当视为没说，实际 %v", got)
	}
}

func TestRootsFromOverrides(t *testing.T) {
	wt := []string{`D:\gleam\worktrees\t1`}
	ctx := WithRoots(context.Background(), wt)
	got := RootsFrom(ctx, []string{`D:\ws`})
	if len(got) != 1 || got[0] != wt[0] {
		t.Errorf("ctx 里有边界时应当以它为准，实际 %v", got)
	}
}

func TestWithRootsCopies(t *testing.T) {
	roots := []string{`D:\a`}
	ctx := WithRoots(context.Background(), roots)
	roots[0] = `D:\b`
	if got := RootsFrom(ctx, nil); got[0] != `D:\a` {
		t.Errorf("ctx 里那份不该跟着外部切片变，实际 %v", got)
	}
}

func TestResolveInRootsWithContextRoots(t *testing.T) {
	wt := t.TempDir()
	other := t.TempDir()
	ctx := WithRoots(context.Background(), []string{wt})

	abs, err := ResolveInRoots("a.txt", RootsFrom(ctx, []string{other}))
	if err != nil {
		t.Fatalf("相对路径应当解析成功: %v", err)
	}
	if want := wt + string('/') + "a.txt"; abs != want && abs != wt+`\a.txt` {
		t.Errorf("相对路径应当基于 ctx 里那个根，实际 %q", abs)
	}
	if _, err := ResolveInRoots(other, RootsFrom(ctx, []string{other})); err == nil {
		t.Error("另一个目录不在 ctx 边界里，应当被拒")
	}
}
