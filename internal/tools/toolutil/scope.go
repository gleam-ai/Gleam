package toolutil

import "context"

type rootsKey struct{}

func WithRoots(ctx context.Context, roots []string) context.Context {
	if ctx == nil || len(roots) == 0 {
		return ctx
	}
	out := make([]string, len(roots))
	copy(out, roots)
	return context.WithValue(ctx, rootsKey{}, out)
}

func RootsFrom(ctx context.Context, fallback []string) []string {
	if ctx != nil {
		if v, ok := ctx.Value(rootsKey{}).([]string); ok && len(v) > 0 {
			return v
		}
	}
	return fallback
}
