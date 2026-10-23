package toolutil

import (
	"fmt"
	"path/filepath"
	"strings"
)

func Within(root, path string) bool {
	root = filepath.Clean(root)
	rel, err := filepath.Rel(root, filepath.Clean(path))
	if err != nil {
		return false
	}
	return rel == "." || (!strings.HasPrefix(rel, "..") && !filepath.IsAbs(rel))
}

func ResolveInRoots(p string, roots []string) (string, error) {
	p = strings.TrimSpace(p)
	if p == "" {
		return "", fmt.Errorf("路径为空")
	}
	if len(roots) == 0 {
		return "", fmt.Errorf("未配置允许的根目录，拒绝访问 %q", p)
	}
	var abs string
	if filepath.IsAbs(p) {
		abs = filepath.Clean(p)
	} else {
		abs = filepath.Clean(filepath.Join(roots[0], p))
	}
	for _, root := range roots {
		checkRoot := root
		if real, err := filepath.EvalSymlinks(root); err == nil {
			checkRoot = real
		}
		checkPath := abs
		if real, err := filepath.EvalSymlinks(abs); err == nil {
			checkPath = real
		} else if parent, err := filepath.EvalSymlinks(filepath.Dir(abs)); err == nil {
			checkPath = filepath.Join(parent, filepath.Base(abs))
		}
		if Within(checkRoot, checkPath) {
			return abs, nil
		}
	}
	return "", fmt.Errorf("路径 %q 超出允许的工作区范围", p)
}
