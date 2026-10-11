package feedback

import (
	"bytes"
	"encoding/json"
	"image"
	"image/png"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"gleam/pkg/types"
)

func newStore(t *testing.T) *Store {
	t.Helper()
	return NewStore(t.TempDir())
}

func sample(id string) *types.Feedback {
	return &types.Feedback{
		ID:        id,
		Kind:      types.FeedbackBug,
		Text:      "点停止按钮没反应",
		Delivery:  types.FeedbackLocalOnly,
		CreatedAt: time.Now(),
	}
}

func pngBytes(t *testing.T) []byte {
	t.Helper()
	var buf bytes.Buffer
	if err := png.Encode(&buf, image.NewRGBA(image.Rect(0, 0, 1, 1))); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func TestSaveThenRead(t *testing.T) {
	s := newStore(t)
	id := NewID()
	want := sample(id)
	if err := s.Save(want); err != nil {
		t.Fatal(err)
	}
	got, err := s.Read(id)
	if err != nil {
		t.Fatal(err)
	}
	if got == nil || got.Text != want.Text || got.Kind != want.Kind {
		t.Fatalf("读回来的不是写进去的那条：%+v", got)
	}
	residue, _ := filepath.Glob(filepath.Join(s.dir(), "*.tmp*"))
	if len(residue) != 0 {
		t.Errorf("feedback/ 留下临时文件: %v", residue)
	}
}

func TestUnsafeIDWritesNothing(t *testing.T) {
	s := newStore(t)
	ids := []string{"../../escape", `..\\..\\x`, "", "fb-1", NewID() + "x", strings.Repeat("x", 200), ".."}
	for _, id := range ids {
		if err := s.Save(sample(id)); err == nil {
			t.Errorf("越界 id 却被接受了: %q", id)
		}
	}
	if _, err := os.Stat(s.dir()); !os.IsNotExist(err) {
		files, _ := os.ReadDir(s.dir())
		t.Errorf("越界 id 留下了东西: %d 项（%v）", len(files), files)
	}
}

func TestRead_MissingVsBroken(t *testing.T) {
	s := newStore(t)
	got, err := s.Read(NewID())
	if err != nil || got != nil {
		t.Fatalf("没写过应当是 (nil, nil)，实得 (%v, %v)", got, err)
	}
	id := NewID()
	if err := os.MkdirAll(s.dir(), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(s.dir(), id+".json"), []byte("{坏 JSON"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Read(id); err == nil {
		t.Error("文件坏了必须报出来，不能当成没提交过")
	}
}

func TestRead_RejectsMismatchedFile(t *testing.T) {
	s := newStore(t)
	id := NewID()
	if err := os.MkdirAll(s.dir(), 0o755); err != nil {
		t.Fatal(err)
	}
	other := sample(NewID())
	b, _ := json.MarshalIndent(other, "", " ")
	if err := os.WriteFile(filepath.Join(s.dir(), id+".json"), b, 0o644); err != nil {
		t.Fatal(err)
	}
	got, err := s.Read(id)
	if err == nil {
		t.Errorf("内容对不上文件名却返回了成功：%+v", got)
	}
}

func TestList_OrderLimitSkip(t *testing.T) {
	s := newStore(t)
	base := time.Now().Add(-time.Hour)
	ids := make([]string, 0, 3)
	for i, txt := range []string{"最早", "中间", "最新"} {
		id := NewID()
		f := sample(id)
		f.Text = txt
		f.CreatedAt = base.Add(time.Duration(i) * time.Minute)
		if err := s.Save(f); err != nil {
			t.Fatal(err)
		}
		ids = append(ids, id)
	}
	if err := os.WriteFile(filepath.Join(s.dir(), "broken.json"), []byte("{"), 0o644); err != nil {
		t.Fatal(err)
	}
	list, skipped, err := s.List(10)
	if err != nil {
		t.Fatal(err)
	}
	if len(list) != 3 {
		t.Fatalf("三条都该在，实得 %d", len(list))
	}
	if skipped != 1 {
		t.Errorf("坏文件应计数 1，实得 %d", skipped)
	}
	if list[0].Text != "最新" || list[2].Text != "最早" {
		t.Errorf("没按提交时间倒序: %s / %s", list[0].Text, list[2].Text)
	}
	limited, _, err := s.List(2)
	if err != nil {
		t.Fatal(err)
	}
	if len(limited) != 2 {
		t.Errorf("limit=2 应当只给 2 条，实得 %d", len(limited))
	}
}

func TestList_NoDirIsNotAnError(t *testing.T) {
	list, skipped, err := newStore(t).List(10)
	if err != nil || skipped != 0 || len(list) != 0 {
		t.Fatalf("空目录应当安静地返回空，实得 (%v, %d, %v)", list, skipped, err)
	}
}

func TestAttachment_SniffsRealType(t *testing.T) {
	s := newStore(t)
	id := NewID()
	name, mime, err := s.SaveAttachment(id, pngBytes(t))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasSuffix(name, ".png") || mime != "image/png" {
		t.Errorf("真 PNG 却存成了 %q / %q", name, mime)
	}
	name2, _, err := s.SaveAttachment(id, pngBytes(t))
	if err != nil || name2 == name {
		t.Errorf("第二张应当换个名字，实得 %q vs %q", name2, name)
	}

	if _, _, err := s.SaveAttachment(id, []byte("#!/bin/sh\nrm -rf /\n")); err == nil {
		t.Error("声称是图片的脚本被存下了")
	}
	if _, _, err := s.SaveAttachment(id, nil); err == nil {
		t.Error("空截图被存下了")
	}
	if _, _, err := s.SaveAttachment(NewID()+"9", pngBytes(t)); err == nil {
		t.Error("越界 id 的截图被存下了")
	}
}

func TestAttachment_ReadRejectsTraversal(t *testing.T) {
	s := newStore(t)
	dataDir := filepath.Dir(s.dir())
	if err := os.WriteFile(filepath.Join(dataDir, "settings.json"), []byte("api_key: secret"), 0o600); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"../../settings.json", `..\\settings.json`, "settings.json", ""} {
		b, _, err := s.ReadAttachment(name)
		if err != nil {
			t.Fatalf("%q 不该报错: %v", name, err)
		}
		if b != nil {
			t.Errorf("%q 越界读到了东西（%d 字节）", name, len(b))
		}
	}
}

func TestDelete_RemovesAttachments(t *testing.T) {
	s := newStore(t)
	id := NewID()
	if err := s.Save(sample(id)); err != nil {
		t.Fatal(err)
	}
	if _, _, err := s.SaveAttachment(id, pngBytes(t)); err != nil {
		t.Fatal(err)
	}
	if err := s.Delete(id); err != nil {
		t.Fatal(err)
	}
	left, _ := filepath.Glob(filepath.Join(s.dir(), id+"*"))
	if len(left) != 0 {
		t.Errorf("删不干净，还剩: %v", left)
	}
	if err := s.Delete("../../etc"); err != nil {
		t.Errorf("越界 id 的删除不该报错，应视为没东西可删: %v", err)
	}
}
