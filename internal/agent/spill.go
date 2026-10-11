package agent

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"gleam/internal/atomicfile"
	"gleam/internal/tools/toolutil"
)

const spillDirName = "tool-output"

func SpillRoot(dataDir string) string {
	if strings.TrimSpace(dataDir) == "" {
		return ""
	}
	return filepath.Join(dataDir, spillDirName)
}

const spillMaxTasks = 200

const spillMaxSegLen = 64

func safeSeg(s string) string {
	s = strings.TrimSpace(s)
	var b strings.Builder
	for _, r := range s {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9':
			b.WriteRune(r)
		case r == '-', r == '_', r == '.':
			b.WriteRune(r)
		default:
			b.WriteRune('_')
		}
	}
	out := strings.Trim(b.String(), ".")
	if len(out) > spillMaxSegLen {
		out = out[:spillMaxSegLen]
	}
	out = strings.Trim(out, ".")
	if out == "" {
		return "_"
	}
	return out
}

func SpillOutput(dataDir, taskID, stepID, content string) (string, error) {
	root := SpillRoot(dataDir)
	if root == "" {
		return "", fmt.Errorf("未配置数据目录")
	}
	dir := filepath.Join(root, safeSeg(taskID))
	path := filepath.Join(dir, safeSeg(stepID)+".txt")
	if _, err := toolutil.ResolveInRoots(path, []string{root}); err != nil {
		return "", err
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}
	if err := atomicfile.Write(path, []byte(content), 0o644); err != nil {
		return "", err
	}
	return path, nil
}

func PruneSpill(dataDir string, keep int) {
	pruneTaskDirs(SpillRoot(dataDir), keep)
}

func pruneTaskDirs(root string, keep int) {
	if root == "" || keep <= 0 {
		return
	}
	entries, err := os.ReadDir(root)
	if err != nil {
		return
	}
	type dirInfo struct {
		path string
		mod  time.Time
	}
	dirs := make([]dirInfo, 0, len(entries))
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		info, err := e.Info()
		if err != nil {
			continue
		}
		dirs = append(dirs, dirInfo{path: filepath.Join(root, e.Name()), mod: info.ModTime()})
	}
	if len(dirs) <= keep {
		return
	}
	sort.Slice(dirs, func(i, j int) bool { return dirs[i].mod.After(dirs[j].mod) })
	for _, d := range dirs[keep:] {
		_ = os.RemoveAll(d.path)
	}
}
