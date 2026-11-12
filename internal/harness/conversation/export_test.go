package conversation

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// TestExportImport tests the conversation export/import functionality.
func TestExportImport(t *testing.T) {
	// Setup two stores: source and destination
	srcDir := t.TempDir()
	dstDir := t.TempDir()

	src, err := Open(srcDir)
	if err != nil {
		t.Fatalf("Open source store: %v", err)
	}

	dst, err := Open(dstDir)
	if err != nil {
		t.Fatalf("Open destination store: %v", err)
	}

	// Create test conversations in source store
	c1, _ := src.Create()
	src.Append(c1.ID, Message{Role: "user", Content: "Hello", CreatedAt: time.Now()})
	src.Append(c1.ID, Message{Role: "assistant", Content: "Hi there!", CreatedAt: time.Now()})

	c2, _ := src.Create()
	src.Append(c2.ID, Message{Role: "user", Content: "Test message", CreatedAt: time.Now()})

	// Export to file
	exportPath := filepath.Join(t.TempDir(), "export.json")
	if err := src.Export(exportPath, ExportOptions{}); err != nil {
		t.Fatalf("Export: %v", err)
	}

	// Verify export file exists and has content
	data, err := os.ReadFile(exportPath)
	if err != nil {
		t.Fatal("Read export file:", err)
	}
	if !strings.Contains(string(data), "Hello") {
		t.Error("Export doesn't contain expected content")
	}

	// Import into destination store
	imported, err := dst.Import(exportPath)
	if err != nil {
		t.Fatalf("Import: %v", err)
	}

	// Should import all 2 conversations
	if imported != 2 {
		t.Errorf("Imported=%d, want 2", imported)
	}

	// Verify destination has the data
	list, err := dst.List()
	if err != nil {
		t.Fatalf("List: %v", err)
	}

	if len(list) != 2 {
		t.Errorf("Destination has %d conversations, want 2", len(list))
	}
}

// TestExportWithFilters tests export filtering by SpaceID and Limit.
func TestExportWithFilters(t *testing.T) {
	storeDir := t.TempDir()
	s, _ := Open(storeDir)

	// Create conversations with different SpaceIDs
	_, _ = s.Create("space-a")
	_, _ = s.Create("space-b")
	_, _ = s.Create("space-a")

	// Export only space-a
	exportPath := filepath.Join(t.TempDir(), "filter-test.json")
	opts := ExportOptions{
		SpaceID: "space-a",
		Limit:   1,
	}
	if err := s.Export(exportPath, opts); err != nil {
		t.Fatalf("Export with filters: %v", err)
	}

	var result struct {
		Count int `json:"count"`
	}
	if err := json.Unmarshal(loadFile(exportPath), &result); err != nil {
		t.Fatal(err)
	}

	if result.Count != 1 {
		t.Errorf("Filtered count=%d, want 1", result.Count)
	}
}

// TestImportInvalidFormat tests error handling for invalid imports.
func TestImportInvalidFormat(t *testing.T) {
	storeDir := t.TempDir()
	s, _ := Open(storeDir)

	// Invalid JSON
	badJSON := filepath.Join(t.TempDir(), "invalid.json")
	os.WriteFile(badJSON, []byte(`{invalid json}`), 0o644)

	if _, err := s.Import(badJSON); err == nil {
		t.Error("Expected error for invalid JSON")
	}

	// Wrong version
	badVersion := filepath.Join(t.TempDir(), "badversion.json")
	os.WriteFile(badVersion, []byte(`{"version":99,"count":0,"conversations":[]}`), 0o644)

	if _, err := s.Import(badVersion); err == nil {
		t.Error("Expected error for unsupported version")
	}
}

// Helper functions
func loadFile(path string) []byte {
	data, _ := os.ReadFile(path)
	return data
}
