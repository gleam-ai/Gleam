package memory

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestManager_CompressDoesNotBlockOtherCalls(t *testing.T) {
	m, _ := Open(t.TempDir(), 2, 64, 100)
	for i := 0; i < 5; i++ {
		m.AddTurn("user", fmt.Sprintf("第 %d 轮的对话内容", i+1))
	}
	entered := make(chan struct{})
	release := make(chan struct{})
	done := make(chan bool, 1)
	go func() {
		done <- m.Compress(func(string) (string, error) {
			close(entered)
			<-release
			return "压缩后的摘要内容", nil
		})
	}()
	<-entered
	called := make(chan struct{})
	go func() {
		m.AddTurn("user", "压缩期间的新一轮对话")
		_ = m.Summary()
		close(called)
	}()
	select {
	case <-called:
	case <-time.After(time.Second):
		close(release)
		t.Fatal("压缩调模型期间记忆读写被全局锁死")
	}
	close(release)
	if !<-done {
		t.Fatal("压缩应成功")
	}
	if !strings.Contains(m.Summary(), "压缩后的摘要内容") {
		t.Errorf("摘要 = %q", m.Summary())
	}
}

func TestManager_CompressDropsStaleSummaryAfterClear(t *testing.T) {
	m, _ := Open(t.TempDir(), 2, 64, 100)
	for i := 0; i < 5; i++ {
		m.AddTurn("user", fmt.Sprintf("旧会话第 %d 轮", i+1))
	}
	entered := make(chan struct{})
	release := make(chan struct{})
	done := make(chan bool, 1)
	go func() {
		done <- m.Compress(func(string) (string, error) {
			close(entered)
			<-release
			return "旧会话的摘要", nil
		})
	}()
	<-entered
	m.ClearSummary()
	close(release)
	if <-done {
		t.Error("清空上下文后，在途压缩结果不该写回")
	}
	if s := m.Summary(); s != "" {
		t.Errorf("新上下文不该带旧摘要: %q", s)
	}
}

func TestManager_PinSurvivesCompress(t *testing.T) {
	m, _ := Open(t.TempDir(), 2, 64, 100)
	m.Pin("输出必须是 Markdown 表格")
	m.Pin("永远不要动 /etc 目录")
	m.AddTurn("user", "第一轮")
	m.AddTurn("assistant", "第二轮")
	m.AddTurn("user", "触发溢出的第三轮")
	if !m.Compress(nil) {
		t.Fatal("应发生压缩")
	}
	pins := m.Pinned()
	if len(pins) != 2 {
		t.Fatalf("钉住区 = %v，压缩不该动它", pins)
	}
	if pins[0] != "输出必须是 Markdown 表格" || pins[1] != "永远不要动 /etc 目录" {
		t.Errorf("钉住区顺序被重排: %v", pins)
	}
}

func TestManager_PinDedupAndCap(t *testing.T) {
	m, _ := Open(t.TempDir(), 2, 64, 100)
	m.Pin("同一条")
	m.Pin("同一条")
	m.Pin("  同一条  ")
	if got := len(m.Pinned()); got != 1 {
		t.Fatalf("重复 Pin 后钉住区 = %d 条，应为 1", got)
	}
	for i := 0; i < pinnedCap+5; i++ {
		m.Pin(string(rune('a'+i%26)) + " 约束 " + string(rune('0'+i%10)))
	}
	if got := len(m.Pinned()); got != pinnedCap {
		t.Fatalf("钉住区 = %d 条，应封顶 %d", got, pinnedCap)
	}
}

func TestManager_AddTurn_PinHint(t *testing.T) {
	m, _ := Open(t.TempDir(), 4, 64, 100)
	m.AddTurn("user", "以后所有回复都用中文标题")
	m.AddTurn("assistant", "好的")
	m.AddTurn("user", "今天天气怎么样")
	if got := m.Pinned(); len(got) != 1 || !strings.Contains(got[0], "以后") {
		t.Fatalf("钉住区 = %v，应只含约束轮", got)
	}
}

func TestManager_SummaryAnnotated_Omission(t *testing.T) {
	m, _ := Open(t.TempDir(), 2, 64, 100)
	if s := m.SummaryAnnotated(); s != "" {
		t.Fatalf("空摘要不该有标注: %q", s)
	}
	for i := 0; i < 5; i++ {
		m.AddTurn("user", fmt.Sprintf("第 %d 轮的对话内容", i+1))
	}
	m.Compress(nil)
	turns, runes := m.Omission()
	if turns != 3 {
		t.Errorf("省略轮数 = %d，应为 3", turns)
	}
	if runes == 0 {
		t.Error("省略字数不应为 0")
	}
	s := m.SummaryAnnotated()
	if !strings.Contains(s, "省略 3 轮") || !strings.Contains(s, "约 ") {
		t.Errorf("摘要标注缺省略量: %q", s)
	}
}

func TestManager_PinAndOmissionPersisted(t *testing.T) {
	dir := t.TempDir()
	m, _ := Open(dir, 2, 64, 100)
	m.Pin("持久化的约束")
	for i := 0; i < 5; i++ {
		m.AddTurn("user", fmt.Sprintf("第 %d 轮的对话内容", i+1))
	}
	m.Compress(nil)

	m2, err := Open(dir, 2, 64, 100)
	if err != nil {
		t.Fatal(err)
	}
	pins := m2.Pinned()
	if len(pins) != 1 || pins[0] != "持久化的约束" {
		t.Fatalf("重开后钉住区 = %v", pins)
	}
	if turns, _ := m2.Omission(); turns != 3 {
		t.Errorf("重开后省略轮数 = %d，应为 3", turns)
	}
}

func TestManager_OldContextFileBackwardCompat(t *testing.T) {
	dir := t.TempDir()
	old := map[string]any{
		"overflow":     []any{},
		"summary":      "旧版摘要",
		"saved_tokens": 42,
	}
	b, _ := json.Marshal(old)
	if err := os.WriteFile(filepath.Join(dir, "memory", contextFile), b, 0o644); err != nil {
		_ = os.MkdirAll(filepath.Join(dir, "memory"), 0o755)
		if err := os.WriteFile(filepath.Join(dir, "memory", contextFile), b, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	m, err := Open(dir, 2, 64, 100)
	if err != nil {
		t.Fatalf("旧状态文件应能读取: %v", err)
	}
	if !strings.Contains(m.Summary(), "旧版摘要") {
		t.Errorf("摘要 = %q", m.Summary())
	}
}

func TestStore_RememberDedup(t *testing.T) {
	s, _ := OpenStore(filepath.Join(t.TempDir(), "lt.json"), 64, 100)
	id1, err := s.Remember("任务记录[success] 目标：整理下载目录的文件 结果：完成（完成度 95）", []string{"task"})
	if err != nil {
		t.Fatal(err)
	}
	id2, err := s.Remember("任务记录[success] 目标：整理下载目录的文件 结果：完成（完成度 96）", []string{"task"})
	if err != nil {
		t.Fatal(err)
	}
	if id1 != id2 {
		t.Errorf("近似重复应更新原条目，却新增了 %s → %s", id1, id2)
	}
	if got := s.Count(); got != 1 {
		t.Errorf("去重后条目数 = %d，应为 1", got)
	}
}

func TestStore_NearDuplicateFromTaskDoesNotOverwriteUserMemory(t *testing.T) {
	s, _ := OpenStore(filepath.Join(t.TempDir(), "lt.json"), 128, 100)
	user := "项目约定：用户偏好是部署到测试环境时把端口固定为 8080，不要改"
	if _, err := s.Remember(user, nil); err != nil {
		t.Fatal(err)
	}
	if _, err := s.RememberScoped("项目约定：用户偏好是部署到测试环境时把端口固定为 9090，不要改", "task", "", nil); err != nil {
		t.Fatal(err)
	}
	if got := s.Count(); got != 2 {
		t.Fatalf("低可信来源不该抹掉用户记忆，条目数 = %d", got)
	}
	var sawUser, sawTask bool
	for _, h := range s.Search("部署端口", 10) {
		sawUser = sawUser || strings.Contains(h.Content, "8080")
		sawTask = sawTask || strings.Contains(h.Content, "9090")
	}
	if !sawUser || !sawTask {
		t.Errorf("两条事实都应可检索：user=%v task=%v", sawUser, sawTask)
	}
}

func TestStore_RejectsOversizeAndTrimsTags(t *testing.T) {
	s, _ := OpenStore(filepath.Join(t.TempDir(), "lt.json"), 64, 100)
	if _, err := s.Remember(strings.Repeat("长", maxMemoryRunes+1), nil); err == nil {
		t.Fatal("超过上限的记忆应被拒绝，而不是静默截断")
	}
	id, err := s.Remember("偏好：深色主题",
		[]string{"  偏好 ", "偏好", "", strings.Repeat("标签", 40)})
	if err != nil {
		t.Fatal(err)
	}
	var got []string
	for _, h := range s.Search("深色主题", 5) {
		if h.ID == id {
			got = h.Tags
		}
	}
	if len(got) != 2 {
		t.Errorf("标签应去重限长，得到 %d 个：%v", len(got), got)
	}
	if len([]rune(got[1])) > maxTagRunes {
		t.Errorf("超长标签未截断：%q", got[1])
	}
}

func TestStore_RememberDistinctNotDeduped(t *testing.T) {
	s, _ := OpenStore(filepath.Join(t.TempDir(), "lt.json"), 64, 100)
	if _, err := s.Remember("用户偏好：回复用中文", nil); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Remember("部署脚本位于 /srv/deploy 目录", nil); err != nil {
		t.Fatal(err)
	}
	if got := s.Count(); got != 2 {
		t.Errorf("不同内容不应去重，条目数 = %d", got)
	}
}

func TestStore_SoftDelete_HiddenFromSearch(t *testing.T) {
	s, _ := OpenStore(filepath.Join(t.TempDir(), "lt.json"), 64, 100)
	id, _ := s.Remember("项目部署在华东区的 Kubernetes 集群", nil)
	if _, err := s.Remember("用户偏好：回复用中文", nil); err != nil {
		t.Fatal(err)
	}
	if !s.SoftDelete(id) {
		t.Fatal("首次软删应成功")
	}
	if s.SoftDelete(id) {
		t.Error("重复软删应返回 false")
	}
	hits := s.Search("Kubernetes 集群部署", 5)
	for _, h := range hits {
		if h.ID == id {
			t.Error("软删条目不应出现在检索结果")
		}
	}
	if got := s.Count(); got != 2 {
		t.Errorf("软删不物理移除，条目数 = %d，应为 2", got)
	}
	if got := s.DeletedCount(); got != 1 {
		t.Errorf("软删计数 = %d，应为 1", got)
	}
}

func TestStore_SearchMinScore(t *testing.T) {
	s, _ := OpenStore(filepath.Join(t.TempDir(), "lt.json"), 64, 100)
	if _, err := s.Remember("整理下载目录里的发票扫描件", nil); err != nil {
		t.Fatal(err)
	}
	hits := s.Search("量子色动力学中的渐近自由", 5)
	if len(hits) != 0 {
		t.Errorf("无关检索不该命中: %+v", hits)
	}
	hits = s.Search("下载目录 发票", 5)
	if len(hits) != 1 {
		t.Errorf("相关检索应命中 1 条，实际 %d", len(hits))
	}
}

func TestStore_OldFileBackwardCompat(t *testing.T) {
	path := filepath.Join(t.TempDir(), "lt.json")
	content := "旧版记忆条目"
	old := []map[string]any{
		{
			"id": "old-1", "content": content,
			"created_at": "2025-01-01T00:00:00Z",
			"vec":        embed(content, 64),
		},
	}
	b, _ := json.Marshal(old)
	if err := os.WriteFile(path, b, 0o644); err != nil {
		t.Fatal(err)
	}
	s, err := OpenStore(path, 64, 100)
	if err != nil {
		t.Fatalf("旧文件应能读取: %v", err)
	}
	if s.Count() != 1 {
		t.Fatalf("条目数 = %d", s.Count())
	}
	id2, err := s.Remember("旧版记忆条目", nil)
	if err != nil {
		t.Fatal(err)
	}
	if id2 != "old-1" {
		t.Errorf("旧条目应参与去重，实际新增 %s", id2)
	}
}
