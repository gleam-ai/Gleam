package agent

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"strings"

	"gleam/pkg/types"
)

const (
	wantFile   = "file"
	wantDir    = "dir"
	wantAbsent = "absent"
	wantExists = "exists"
)

type artifactExpectation struct {
	stepID    string
	tool      string
	path      string
	want      string
	exact     int
	atLeast   int
	jsonCheck bool
	content   string
}

type artifactFact struct {
	stepID string
	tool   string
	path   string
	ok     bool
	detail string
	want   string
	size   int64
}

func verifyArtifacts(steps []types.StepResult) []artifactFact {
	order := make([]string, 0, len(steps))
	final := map[string]artifactExpectation{}
	put := func(e artifactExpectation) {
		if e.path == "" {
			return
		}
		if _, seen := final[e.path]; !seen {
			order = append(order, e.path)
		}
		final[e.path] = e
	}

	for _, st := range steps {
		if st.Status != types.StepSucceeded {
			continue
		}
		switch st.Tool {
		case "file.write":
			e := artifactExpectation{
				stepID: st.StepID, tool: st.Tool,
				path: outStr(st.Output, "path"), want: wantFile,
				exact: -1, atLeast: -1,
			}
			if content, ok := st.FinalArgs["content"].(string); ok {
				e.content = content
				if argBool(st.FinalArgs, "append") {
					e.atLeast = len(content)
				} else {
					e.exact = len(content)
					e.jsonCheck = looksJSON(e.path)
				}
			}
			put(e)
		case "file.mkdir":
			put(artifactExpectation{
				stepID: st.StepID, tool: st.Tool,
				path: outStr(st.Output, "path"), want: wantDir,
				exact: -1, atLeast: -1,
			})
		case "file.move":
			put(artifactExpectation{
				stepID: st.StepID, tool: st.Tool,
				path: outStr(st.Output, "src"), want: wantAbsent,
				exact: -1, atLeast: -1,
			})
			put(artifactExpectation{
				stepID: st.StepID, tool: st.Tool,
				path: outStr(st.Output, "dst"), want: wantExists,
				exact: -1, atLeast: -1,
			})
		case "file.delete":
			put(artifactExpectation{
				stepID: st.StepID, tool: st.Tool,
				path: outStr(st.Output, "path"), want: wantAbsent,
				exact: -1, atLeast: -1,
			})
		}
	}

	facts := make([]artifactFact, 0, len(order))
	for _, p := range order {
		facts = append(facts, checkArtifact(final[p]))
	}
	return facts
}

func checkArtifact(e artifactExpectation) artifactFact {
	f := artifactFact{stepID: e.stepID, tool: e.tool, path: e.path, want: e.want}
	fi, err := os.Stat(e.path)
	exists := err == nil
	if exists {
		f.size = fi.Size()
	}

	switch e.want {
	case wantAbsent:
		if exists {
			f.detail = "步骤自述成功，但该路径仍然存在"
			return f
		}
		f.ok = true
		f.detail = "已不存在，与预期一致"
	case wantDir:
		switch {
		case !exists:
			f.detail = "步骤自述成功，但目录不存在"
		case !fi.IsDir():
			f.detail = "步骤自述成功，但该路径存在且不是目录"
		default:
			f.ok = true
			f.detail = "目录存在"
		}
	case wantExists:
		if !exists {
			f.detail = "步骤自述成功，但目标不存在"
			return f
		}
		f.ok = true
		f.detail = "存在"
	default:
		switch {
		case !exists:
			f.detail = "步骤自述成功，但文件不存在"
		case fi.IsDir():
			f.detail = "步骤自述成功，但该路径是目录、不是文件"
		case e.exact >= 0 && fi.Size() != int64(e.exact):
			f.detail = fmt.Sprintf("文件在，但长度不符：实测 %d 字节，写入的内容应为 %d 字节", fi.Size(), e.exact)
		case e.atLeast >= 0 && fi.Size() < int64(e.atLeast):
			f.detail = fmt.Sprintf("文件在，但内容不足：实测 %d 字节，仅追加的内容就有 %d 字节", fi.Size(), e.atLeast)
		default:
			if e.exact >= 0 || e.atLeast >= 0 {
				if broken, why := contentMismatch(e.path, e.content, e.exact >= 0); broken {
					f.detail = why
					return f
				}
			}
			if e.jsonCheck {
				if broken, why := jsonBroken(e.path); broken {
					f.detail = why
					return f
				}
			}
			f.ok = true
			if e.exact >= 0 || e.atLeast >= 0 {
				f.detail = fmt.Sprintf("文件存在，%d 字节，内容回读一致", fi.Size())
			} else {
				f.detail = fmt.Sprintf("文件存在，%d 字节（内容未回读核实）", fi.Size())
			}
		}
	}
	return f
}

func failedArtifacts(facts []artifactFact) []artifactFact {
	var bad []artifactFact
	for _, f := range facts {
		if !f.ok {
			bad = append(bad, f)
		}
	}
	return bad
}

func artifactsSatisfied(facts []artifactFact) bool {
	return len(failedArtifacts(facts)) == 0
}

func artifactFailureNote(bad []artifactFact) string {
	parts := make([]string, 0, len(bad))
	for _, f := range bad {
		parts = append(parts, fmt.Sprintf("%s（%s 步骤 %s）：%s", f.path, f.tool, f.stepID, f.detail))
	}
	return fmt.Sprintf("产物核对未通过 %d 项——%s", len(bad), strings.Join(parts, "；"))
}

func artifactDigest(facts []artifactFact) string {
	if len(facts) == 0 {
		return ""
	}
	var b strings.Builder
	b.WriteString("## 产物核对（代码实测，不是步骤自述）\n")
	for _, f := range facts {
		mark := "[通过]"
		if !f.ok {
			mark = "[不通过]"
		}
		fmt.Fprintf(&b, "- %s %s（%s）→ %s\n", mark, f.path, f.tool, f.detail)
	}
	if bad := failedArtifacts(facts); len(bad) > 0 {
		fmt.Fprintf(&b, "\n以上 %d 项未通过。注意「步骤自述成功」不等于「产物真的在」——判定验收标准时以本段实测为准。\n", len(bad))
	}
	return b.String()
}

func outStr(v any, key string) string {
	m, ok := v.(map[string]any)
	if !ok {
		return ""
	}
	s, _ := m[key].(string)
	return s
}

func argBool(args map[string]any, key string) bool {
	if args == nil {
		return false
	}
	b, _ := args[key].(bool)
	return b
}

func looksJSON(path string) bool {
	p := strings.ToLower(path)
	return strings.HasSuffix(p, ".json") || strings.HasSuffix(p, ".jsonl") || strings.HasSuffix(p, ".ndjson")
}

func jsonBroken(path string) (bool, string) {
	b, err := os.ReadFile(path)
	if err != nil {
		return false, ""
	}
	b = bytes.TrimPrefix(b, []byte{0xEF, 0xBB, 0xBF})
	var v any
	if json.Unmarshal(b, &v) == nil {
		return false, ""
	}
	for _, ln := range bytes.Split(b, []byte("\n")) {
		if len(bytes.TrimSpace(ln)) == 0 {
			continue
		}
		if json.Unmarshal(ln, &v) != nil {
			return true, "文件在、长度也对，但内容不是合法 JSON（格式不合法）"
		}
	}
	return false, ""
}

// contentMismatch reads the file back and compares it to the intended content.
// exact compares the whole file (overwrite); otherwise it checks the appended
// suffix. The caller already checked length, so this catches same-length wrong
// content that a size check alone would pass -- disk state, not self-report.
func contentMismatch(path, content string, exact bool) (bool, string) {
	b, err := os.ReadFile(path)
	if err != nil {
		return true, "文件在，但回读失败：" + err.Error()
	}
	got := string(b)
	if exact {
		if got != content {
			return true, "文件在、长度也对，但回读内容与写入内容不一致"
		}
		return false, ""
	}
	if !strings.HasSuffix(got, content) {
		return true, "文件在，但回读内容末尾不是本次追加的内容"
	}
	return false, ""
}
