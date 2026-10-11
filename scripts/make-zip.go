//go:build ignore

package main

import (
	"archive/zip"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

func main() {
	if len(os.Args) < 3 {
		fmt.Fprintln(os.Stderr, "用法: go run scripts/make-zip.go <out.zip> <entry>...")
		os.Exit(2)
	}
	out := os.Args[1]
	if err := os.MkdirAll(filepath.Dir(out), 0755); err != nil {
		fatal(err)
	}
	f, err := os.Create(out)
	if err != nil {
		fatal(err)
	}
	defer f.Close()

	zw := zip.NewWriter(f)
	for _, entry := range os.Args[2:] {
		src, alias := entry, ""
		if i := strings.Index(entry, "="); i > 0 {
			src, alias = entry[:i], entry[i+1:]
		}
		src = filepath.Clean(src)
		info, err := os.Stat(src)
		if err != nil {
			fatal(err)
		}
		if info.IsDir() {
			if err := addDir(zw, src, alias); err != nil {
				fatal(err)
			}
			continue
		}
		name := alias
		if name == "" {
			name = filepath.Base(src)
		}
		if err := addFile(zw, src, name, info); err != nil {
			fatal(err)
		}
	}
	if err := zw.Close(); err != nil {
		fatal(err)
	}
	if st, err := os.Stat(out); err == nil {
		fmt.Printf("%s  %d bytes\n", out, st.Size())
	}
}

func addDir(zw *zip.Writer, src, alias string) error {
	return filepath.WalkDir(src, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			name := relName(src, path, alias)
			if name == "" || name == "." {
				return nil
			}
			_, err := zw.CreateHeader(&zip.FileHeader{Name: name + "/", Method: zip.Deflate})
			return err
		}
		if skip(d.Name()) {
			return nil
		}
		info, err := d.Info()
		if err != nil {
			return err
		}
		return addFile(zw, path, relName(src, path, alias), info)
	})
}

func addFile(zw *zip.Writer, src, name string, info os.FileInfo) error {
	if name == "" || skip(filepath.Base(name)) {
		return nil
	}
	mode := info.Mode()
	if !mode.IsRegular() {
		return nil
	}
	hdr, err := zip.FileInfoHeader(info)
	if err != nil {
		return err
	}
	hdr.Name = filepath.ToSlash(name)
	hdr.Method = zip.Deflate
	if executable(name, mode) {
		hdr.SetMode(0755)
	} else {
		hdr.SetMode(0644)
	}
	w, err := zw.CreateHeader(hdr)
	if err != nil {
		return err
	}
	f, err := os.Open(src)
	if err != nil {
		return err
	}
	defer f.Close()
	_, err = io.Copy(w, f)
	return err
}

func executable(name string, mode os.FileMode) bool {
	if mode&0111 != 0 {
		return true
	}
	ext := strings.ToLower(filepath.Ext(name))
	return ext == ".exe" || ext == ".sh" || ext == ".command" || ext == ""
}

func relName(root, path, alias string) string {
	rel, err := filepath.Rel(root, path)
	if err != nil {
		return ""
	}
	rel = filepath.ToSlash(rel)
	if alias == "" {
		return rel
	}
	if rel == "." {
		return alias
	}
	return strings.TrimSuffix(alias, "/") + "/" + rel
}

func skip(name string) bool {
	switch name {
	case ".git", ".gitignore", "node_modules", "Thumbs.db", ".DS_Store":
		return true
	}
	return strings.HasSuffix(name, ".tmp")
}

func fatal(err error) {
	fmt.Fprintln(os.Stderr, "错误:", err)
	os.Exit(1)
}
