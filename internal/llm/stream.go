package llm

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"time"
)

var (
	StreamTTFBTimeout  = 30 * time.Second
	StreamStallTimeout = 15 * time.Second
)

var (
	ErrStreamTTFB  = errors.New("llm: 流式响应首字节超时（TTFB）")
	ErrStreamStall = errors.New("llm: 流式响应长时间无数据（块间超时）")
)

func IsStreamTimeout(err error) bool {
	return errors.Is(err, ErrStreamTTFB) || errors.Is(err, ErrStreamStall)
}

func scanStream(ctx context.Context, rc io.Reader, onLine func(line string) bool) error {
	type lineMsg struct {
		line string
		err  error
	}
	lines := make(chan lineMsg)
	go func() {
		defer close(lines)
		scanner := bufio.NewScanner(rc)
		scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)
		for scanner.Scan() {
			select {
			case lines <- lineMsg{line: scanner.Text()}:
			case <-ctx.Done():
				return
			}
		}
		if err := scanner.Err(); err != nil {
			select {
			case lines <- lineMsg{err: err}:
			case <-ctx.Done():
			}
		}
	}()

	wait := StreamTTFBTimeout
	if wait <= 0 {
		wait = 30 * time.Second
	}
	timer := time.NewTimer(wait)
	defer timer.Stop()
	first := true

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-timer.C:
			if first {
				return ErrStreamTTFB
			}
			return ErrStreamStall
		case m, ok := <-lines:
			if !ok {
				return nil
			}
			if m.err != nil {
				return fmt.Errorf("llm: 流式读取中断: %w", m.err)
			}
			first = false
			if !timer.Stop() {
				select {
				case <-timer.C:
				default:
				}
			}
			stall := StreamStallTimeout
			if stall <= 0 {
				stall = 15 * time.Second
			}
			timer.Reset(stall)
			if onLine != nil && !onLine(m.line) {
				return nil
			}
		}
	}
}
