package pkg

import (
	"encoding/json"
	"fmt"
)

// JSONListSchema describes the schema for a list of JSON objects.
// Fields map field names to expected types or nested schemas.
type JSONListSchema struct {
	Fields map[string]json.RawMessage `json:"fields"`
}

// Validate checks that the provided value is a []map[string]any.
func (s *JSONListSchema) Validate(v any) error {
	if v == nil {
		return fmt.Errorf("value cannot be nil")
	}
	list, ok := v.([]any)
	if !ok {
		return fmt.Errorf("expected []any, got %T", v)
	}
	for i, item := range list {
		if _, ok := item.(map[string]any); !ok {
			return fmt.Errorf("item at index %d is not map[string]any", i)
		}
	}
	return nil
}

// ValidateJSONListSchema is the central validation entry point for list-type fields.
func ValidateJSONListSchema(v any) error {
	if v == nil {
		return fmt.Errorf("value cannot be nil")
	}
	list, ok := v.([]any)
	if !ok {
		return fmt.Errorf("expected []any, got %T", v)
	}
	for i, item := range list {
		if _, ok := item.(map[string]any); !ok {
			return fmt.Errorf("item at index %d is not map[string]any", i)
		}
	}
	return nil
}

// MustValidateJSONListSchema panics if invalid; useful in tests or migrations.
func MustValidateJSONListSchema(v any) {
	if err := ValidateJSONListSchema(v); err != nil {
		panic(err)
	}
}
