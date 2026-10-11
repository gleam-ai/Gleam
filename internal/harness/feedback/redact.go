package feedback

import (
	"strings"

	"gleam/pkg/types"
)

const RedactedMarker = "‹已脱敏›"

func Redact(f *types.Feedback, secrets []string) (*types.Feedback, int) {
	if f == nil {
		return nil, 0
	}
	list := make([]string, 0, len(secrets))
	for _, s := range secrets {
		if s != "" {
			list = append(list, s)
		}
	}
	n := 0
	scrub := func(s string) string {
		for _, sec := range list {
			if c := strings.Count(s, sec); c > 0 {
				n += c
				s = strings.ReplaceAll(s, sec, RedactedMarker)
			}
		}
		return s
	}
	out := *f
	out.Text = scrub(out.Text)
	out.Context.AppVersion = scrub(out.Context.AppVersion)
	out.Context.GoVersion = scrub(out.Context.GoVersion)
	out.Context.OS = scrub(out.Context.OS)
	out.Context.Model = scrub(out.Context.Model)
	out.Context.LLMHost = scrub(out.Context.LLMHost)
	out.Context.TaskID = scrub(out.Context.TaskID)
	out.Context.FailedTool = scrub(out.Context.FailedTool)
	return &out, n
}
