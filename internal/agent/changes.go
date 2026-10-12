package agent

import (
	"os"

	"gleam/pkg/types"
)

type changeCtx struct {
	DataDir string
	TaskID  string
}

func buildChanges(steps []types.StepResult, recs []preImage, ctx ...changeCtx) []types.FileChange {
	facts := verifyArtifacts(steps)
	byPath := make(map[string]artifactFact, len(facts))
	display := make(map[string]string, len(facts)+len(recs))
	order := make([]string, 0, len(facts)+len(recs))
	for _, f := range facts {
		key := pathKey(f.path)
		if _, seen := display[key]; !seen {
			order = append(order, key)
			display[key] = f.path
		}
		byPath[key] = f
	}
	for _, r := range recs {
		key := pathKey(r.Path)
		if _, has := display[key]; !has {
			order = append(order, key)
			display[key] = r.Path
		}
	}
	if len(order) == 0 {
		return nil
	}

	out := make([]types.FileChange, 0, len(order))
	for _, key := range order {
		p := display[key]
		f, hasFact := byPath[key]
		rec, hasRec := firstPreImageOf(recs, p)
		ch := types.FileChange{Path: p}
		switch {
		case hasFact:
			ch.Tool, ch.StepID = f.tool, f.stepID
			ch.OK, ch.Note, ch.Bytes = f.ok, f.detail, f.size
			ch.Kind = changeKind(f.tool, f.want, hasRec && rec.Exists)
		case hasRec:
			ch.Tool, ch.StepID = rec.Tool, rec.StepID
			ch.Kind = kindTouched
			ch.Note = "该步骤未成功，产物核对没有结论；写前快照留着，可还原"
		}
		if hasRec {
			ch.PrevBytes = rec.Bytes
			ch.Reversible = rec.reversible()
			if !ch.Reversible {
				ch.Blocked = rec.Reason
				if ch.Blocked == "" {
					ch.Blocked = "写前内容未留存，无法还原"
				}
			}
		} else {
			ch.Blocked = "没有写前快照（未配置数据目录，或这一步在快照之前就被跳过）"
		}
		if !hasFact {
			if info, err := os.Stat(p); err == nil && !info.IsDir() {
				ch.Bytes = info.Size()
			}
		}
		if len(ctx) > 0 {
			ch.AddedLines, ch.DeletedLines = changeLineCounts(ctx[0], rec, p, hasRec)
		}
		out = append(out, ch)
	}
	return out
}

func changeLineCounts(c changeCtx, rec preImage, path string, hasRec bool) (added, deleted int) {
	if !hasRec {
		return 0, 0
	}
	var before string
	if rec.Exists {
		if rec.File == "" {
			return 0, 0
		}
		raw, err := preImageContent(c.DataDir, c.TaskID, rec)
		if err != nil {
			return 0, 0
		}
		before = string(raw)
	}
	var after string
	if raw, err := os.ReadFile(path); err == nil {
		after = string(raw)
	} else if !os.IsNotExist(err) {
		return 0, 0
	}
	res := DiffText(before, after)
	return res.Added, res.Deleted
}

const (
	kindAdded    = "added"
	kindModified = "modified"
	kindDeleted  = "deleted"
	kindDir      = "dir"
	kindMoved    = "moved"
	kindTouched  = "touched"
)

func changeKind(tool, want string, prevExists bool) string {
	switch {
	case tool == "file.move" && want == wantExists:
		return kindMoved
	case want == wantAbsent:
		if prevExists {
			return kindDeleted
		}
		return kindTouched
	case want == wantDir:
		return kindDir
	case want == wantFile:
		if prevExists {
			return kindModified
		}
		return kindAdded
	default:
		return kindTouched
	}
}
