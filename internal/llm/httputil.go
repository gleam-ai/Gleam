package llm

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
)

const maxBodyBytes = 16 << 20

func StatusCode(err error) int {
	var se *statusError
	if errors.As(err, &se) {
		return se.code
	}
	return 0
}

func normalizeBase(base string, suffixes ...string) string {
	base = strings.TrimRight(base, "/")
	changed := true
	for changed {
		changed = false
		for _, s := range suffixes {
			if strings.HasSuffix(base, s) && len(base) > len(s) {
				base = strings.TrimSuffix(base, s)
				changed = true
			}
		}
	}
	return base
}

func postJSONOnce(ctx context.Context, hc *http.Client, url string, headers map[string]string, body []byte) ([]byte, error) {
	noteEgress(url, len(body))
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	resp, err := hc.Do(req)
	if err != nil {
		return nil, fmt.Errorf("llm: 请求失败: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		snippet, _ := io.ReadAll(io.LimitReader(resp.Body, 2048))
		return nil, &statusError{code: resp.StatusCode, msg: string(snippet)}
	}
	return io.ReadAll(io.LimitReader(resp.Body, maxBodyBytes))
}

func postJSON(ctx context.Context, hc *http.Client, url string, headers map[string]string, body []byte) ([]byte, error) {
	return DoWithRetry(ctx, DefaultRetryConfig, func(err error) bool { return retryable(ctx, err) }, func() ([]byte, error) {
		return postJSONOnce(ctx, hc, url, headers, body)
	})
}

func openStreamOnce(ctx context.Context, hc *http.Client, url string, headers map[string]string, body []byte) (io.ReadCloser, error) {
	noteEgress(url, len(body))
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "text/event-stream")
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	resp, err := hc.Do(req)
	if err != nil {
		return nil, fmt.Errorf("llm: 请求失败: %w", err)
	}
	if resp.StatusCode != http.StatusOK {
		defer resp.Body.Close()
		snippet, _ := io.ReadAll(io.LimitReader(resp.Body, 2048))
		return nil, &statusError{code: resp.StatusCode, msg: string(snippet)}
	}
	return resp.Body, nil
}

func openStream(ctx context.Context, hc *http.Client, url string, headers map[string]string, body []byte) (io.ReadCloser, error) {
	return DoWithRetry(ctx, DefaultRetryConfig, func(err error) bool { return retryable(ctx, err) }, func() (io.ReadCloser, error) {
		return openStreamOnce(ctx, hc, url, headers, body)
	})
}
