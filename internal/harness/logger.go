package harness

import (
	"encoding/json"
	"os"
	"time"
)

// StructLogger outputs structured JSON logs to stderr.
type StructLogger struct {
	out *os.File
}

func NewStructLogger() *StructLogger {
	return &StructLogger{out: os.Stderr}
}

func (l *StructLogger) Log(level, msg string, fields any) {
	entry := map[string]any{
		"time":    time.Now().UTC().Format(time.RFC3339),
		"level":   level,
		"message": msg,
	}
	if fields != nil {
		for k, v := range fields.(map[string]any) {
			entry[k] = v
		}
	}
	b, _ := json.Marshal(entry)
	l.out.Write(append(b, '\n'))
}

func (l *StructLogger) Info(msg string, fields any) {
	l.Log("info", msg, fields)
}

func (l *StructLogger) Error(msg string, fields any) {
	l.Log("error", msg, fields)
}

func (l *StructLogger) Debug(msg string, fields any) {
	l.Log("debug", msg, fields)
}
