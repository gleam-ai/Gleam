package conversation

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"gleam/internal/atomicfile"
)

// ExportOptions configures how conversations are exported.
type ExportOptions struct {
	IncludeMetadata bool   // 包含创建/更新时间戳
	SpaceID         string // 只导出特定 SpaceID 的会话
	Limit           int    // 最多导出多少条会话
}

// Export exports conversations to a JSON file.
func (s *Store) Export(path string, opts ExportOptions) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	entries, err := os.ReadDir(s.dir)
	if err != nil {
		return fmt.Errorf("读取会话目录失败：%w", err)
	}

	var exportList []*Conversation
	for _, e := range entries {
		if e.IsDir() || filepath.Ext(e.Name()) != ".json" {
			continue
		}

		c, err := s.readLocked(filepath.Join(s.dir, e.Name()))
		if err != nil {
			continue
		}

		// Apply filters
		if opts.SpaceID != "" && c.SpaceID != opts.SpaceID {
			continue
		}
		if opts.Limit > 0 && len(exportList) >= opts.Limit {
			break
		}

		exportList = append(exportList, c)
	}

	// Create export structure
	exportData := map[string]interface{}{
		"version":       1,
		"count":         len(exportList),
		"conversations": exportList,
	}

	// Marshal to JSON
	out, err := json.MarshalIndent(exportData, "", "  ")
	if err != nil {
		return fmt.Errorf("编码导出数据失败：%w", err)
	}

	// Write to file
	if err := atomicfile.Write(path, out, 0o644); err != nil {
		return fmt.Errorf("写入导出文件失败：%w", err)
	}

	return nil
}

// Import imports conversations from a JSON file.
// Returns the number of conversations imported.
func (s *Store) Import(path string) (int, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return 0, fmt.Errorf("读取导入文件失败：%w", err)
	}

	var importData struct {
		Version       int               `json:"version"`
		Count         int               `json:"count"`
		Conversations []ConversationRaw `json:"conversations"`
	}

	if err := json.Unmarshal(data, &importData); err != nil {
		return 0, fmt.Errorf("解析导入数据失败：%w", err)
	}

	if importData.Version != 1 {
		return 0, fmt.Errorf("不支持的导入格式版本 %d", importData.Version)
	}

	imported := 0
	for _, cr := range importData.Conversations {
		c, err := convertConversationRaw(&cr)
		if err != nil {
			continue
		}

		// Generate new ID for each conversation
		newID := NewImportID(c.Title)

		s.mu.Lock()
		err = s.writeConversationLocked(newID, c)
		s.mu.Unlock()

		if err == nil {
			imported++
		}
	}

	return imported, nil
}

// ConversationRaw is the JSON structure for importing conversations.
type ConversationRaw struct {
	ID        string      `json:"id"`
	Title     string      `json:"title"`
	SpaceID   string      `json:"space_id,omitempty"`
	Messages  []Message   `json:"messages"`
	CreatedAt interface{} `json:"created_at"`
	UpdatedAt interface{} `json:"updated_at"`
}

// convertConversationRaw converts a raw conversation import to a proper Conversation.
func convertConversationRaw(cr *ConversationRaw) (*Conversation, error) {
	c := &Conversation{
		Title:   cr.Title,
		SpaceID: cr.SpaceID,
	}

	// Parse timestamps if present
	if cr.CreatedAt != nil {
		if ts, ok := cr.CreatedAt.(string); ok {
			if t, err := time.Parse(time.RFC3339, ts); err == nil {
				c.CreatedAt = t
			}
		}
	}

	if cr.UpdatedAt != nil {
		if ts, ok := cr.UpdatedAt.(string); ok {
			if t, err := time.Parse(time.RFC3339, ts); err == nil {
				c.UpdatedAt = t
			}
		}
	}

	// Clone messages
	c.Messages = make([]Message, len(cr.Messages))
	copy(c.Messages, cr.Messages)

	return c, nil
}

// exportConversationToFile exports a single conversation to a JSON file.
func exportConversationToFile(path string, c *Conversation) error {
	out, err := json.MarshalIndent(c, "", "  ")
	if err != nil {
		return fmt.Errorf("编码会话失败：%w", err)
	}
	return atomicfile.Write(path, out, 0o644)
}
