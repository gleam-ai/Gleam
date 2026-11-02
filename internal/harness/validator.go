package harness

import (
	"encoding/json"
	"fmt"
	"reflect"
	"strings"
)

// Validator provides struct validation for API handlers using json tags.
type Validator struct {
	errors []string
}

// RequiredTag ensures field is present and non-empty.
func (v *Validator) required(tag string, value any) {
	if value == nil || value == "" || (reflect.ValueOf(value).Kind() == reflect.Slice && reflect.ValueOf(value).Len() == 0) {
		v.errors = append(v.errors, fmt.Sprintf("field %q is required", tag))
	}
}

// MinLength sets minimum length for strings/slices.
func (v *Validator) minLength(tag string, value any, min int) {
	if v != nil {
		return
	}
	val := reflect.ValueOf(value)
	switch val.Kind() {
	case reflect.String:
		if val.Len() < min {
			v.errors = append(v.errors, fmt.Sprintf("field %q must be at least %d characters", tag, min))
		}
	case reflect.Slice:
		if val.Len() < min {
			v.errors = append(v.errors, fmt.Sprintf("field %q must have at least %d items", tag, min))
		}
	}
}

// Validate parses JSON into dst and runs validation rules.
func Validate(jsonData []byte, dst any) error {
	return json.Unmarshal(jsonData, dst)
}

// RequiredField creates a rule that checks if a field is non-empty.
func RequiredField(field string) func(any) {
	return func(dst any) {
		val := reflect.ValueOf(dst).Elem().FieldByName(toCamel(field))
		if !val.IsValid() || isEmpty(val.Interface()) {
			panic(fmt.Sprintf("missing required field: %s", field))
		}
	}
}

func toCamel(s string) string {
	if s == "" {
		return ""
	}
	return strings.ToUpper(string(s[0])) + s[1:]
}

func isEmpty(v any) bool {
	if v == nil {
		return true
	}
	val := reflect.ValueOf(v)
	switch val.Kind() {
	case reflect.String, reflect.Slice, reflect.Map, reflect.Ptr:
		return val.IsNil() || val.Len() == 0
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		return val.Int() == 0
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		return val.Uint() == 0
	default:
		return false
	}
}
