package agent

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"gleam/internal/atomicfile"
	"gleam/pkg/types"
)

func TaskArchivePath(dataDir, taskID string) string {
	name := SafeTaskName(taskID)
	if name == "" {
		return ""
	}
	return filepath.Join(dataDir, "tasks", name+".json")
}

func SaveTaskResult(dataDir string, result *types.GoalResult) error {
	if result == nil {
		return fmt.Errorf("没有可归档的任务结果")
	}
	if dataDir == "" {
		return fmt.Errorf("未配置数据目录，任务 %s 未归档", result.TaskID)
	}
	path := TaskArchivePath(dataDir, result.TaskID)
	if path == "" {
		return fmt.Errorf("任务 ID %q 不能作文件名，未归档", result.TaskID)
	}
	b, err := json.MarshalIndent(result, "", " ")
	if err != nil {
		return fmt.Errorf("序列化任务结果失败: %w", err)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	if err := atomicfile.Write(path, b, 0o644); err != nil {
		return err
	}
	DiscardRunLog(dataDir, result.TaskID)
	return nil
}

func ListTaskResults(dataDir string, limit int) ([]*types.GoalResult, int, error) {
	if limit <= 0 {
		limit = 50
	}
	dir := filepath.Join(dataDir, "tasks")
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, 0, nil
		}
		return nil, 0, err
	}
	type entry struct {
		id  string
		mod time.Time
	}
	var files []entry
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".json") {
			continue
		}
		info, err := e.Info()
		if err != nil {
			continue
		}
		files = append(files, entry{strings.TrimSuffix(e.Name(), ".json"), info.ModTime()})
	}
	sort.Slice(files, func(i, j int) bool { return files[i].mod.After(files[j].mod) })
	out := make([]*types.GoalResult, 0, limit)
	skipped := 0
	for _, f := range files {
		if len(out) >= limit {
			break
		}
		g, err := ReadTaskResult(dataDir, f.id)
		if err != nil || g == nil {
			skipped++
			continue
		}
		out = append(out, g)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].StartedAt.After(out[j].StartedAt) })
	return out, skipped, nil
}

func ReadTaskResult(dataDir, taskID string) (*types.GoalResult, error) {
	path := TaskArchivePath(dataDir, taskID)
	if path == "" {
		return nil, nil
	}
	b, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	var g types.GoalResult
	if err := json.Unmarshal(b, &g); err != nil {
		return nil, fmt.Errorf("解析任务记录失败（%s）: %w", path, err)
	}
	if g.TaskID != taskID {
		return nil, fmt.Errorf("任务记录内容与文件名不符（%s 里写的是 %q）", path, g.TaskID)
	}
	return &g, nil
}
