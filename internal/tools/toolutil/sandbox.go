package toolutil

import (
	"fmt"
	"os"
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
	checkPath, err := resolveExistingAncestor(abs)
	if err != nil {
		return "", fmt.Errorf("无法解析路径 %q: %w", p, err)
	}
	for _, root := range roots {
		checkRoot, err := resolveExistingAncestor(root)
		if err != nil {
			return "", fmt.Errorf("无法解析工作区根目录 %q: %w", root, err)
		}
		if Within(checkRoot, checkPath) {
			return abs, nil
		}
	}
	return "", fmt.Errorf("路径 %q 超出允许的工作区范围", p)
}

func resolveExistingAncestor(path string) (string, error) {
	var suffix []string
	for {
		_, err := os.Lstat(path)
		if err == nil {
			// Lstat stops at dangling links too; unresolved links must not fall
			// back to a lexical path that could hide an external destination.
			real, err := resolveExistingPath(path)
			if err != nil {
				return "", err
			}
			for i := len(suffix) - 1; i >= 0; i-- {
				real = filepath.Join(real, suffix[i])
			}
			return real, nil
		}
		if !os.IsNotExist(err) {
			return "", err
		}
		parent := filepath.Dir(path)
		if parent == path {
			return "", err
		}
		suffix = append(suffix, filepath.Base(path))
		path = parent
	}
}
