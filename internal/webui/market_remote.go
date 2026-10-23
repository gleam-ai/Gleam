package webui

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"gleam/internal/atomicfile"
	"gleam/internal/buildinfo"
	"gleam/internal/market"
)

const (
	marketFetchTimeout = 15 * time.Second
	marketProbeTimeout = 3 * time.Second
	marketMaxBody      = 4 << 20
	marketCacheName    = "market-official-mcp.json"
	marketCacheTTL     = 10 * time.Minute
	// Search terms come from user input; the cache key space must be bounded,
	// or it grows forever in a long-running process.
	marketQueryCacheMax = 64
)

type marketState struct {
	mu      sync.Mutex
	byQuery map[string]marketEntry
	npmPick string
	npmAt   time.Time
	npmWhy  string
}

type marketEntry struct {
	presets []market.RemotePreset
	at      time.Time
}

var marketCacheState = &marketState{}

func (s *Server) handleMarketSources(w http.ResponseWriter, _ *http.Request) {
	srcs := market.DefaultSources()
	pick, why := s.marketNPMSource(context.Background())
	writeJSON(w, 200, map[string]any{
		"sources":        srcs,
		"npm_source":     pick.ID,
		"npm_source_why": why,
		"catalog_source": "official-mcp",
		"note": "目录来自 MCP 官方注册表（没有国内镜像）；npm 源只影响安装时 npx 走哪个 registry，" +
			"选择方式是实测这两个地址的响应延迟，不查 IP 归属地。",
	})
}

func (s *Server) marketNPMSource(ctx context.Context) (market.Source, string) {
	cands := []market.Source{}
	for _, src := range market.DefaultSources() {
		if src.Kind == "npm" {
			cands = append(cands, src)
		}
	}
	if len(cands) == 0 {
		return market.Source{}, "没有配置 npm 源"
	}

	marketCacheState.mu.Lock()
	if marketCacheState.npmPick != "" && time.Since(marketCacheState.npmAt) < marketCacheTTL {
		id, why := marketCacheState.npmPick, marketCacheState.npmWhy
		marketCacheState.mu.Unlock()
		for _, c := range cands {
			if c.ID == id {
				return c, why
			}
		}
	} else {
		marketCacheState.mu.Unlock()
	}

	best := cands[0]
	bestDur := time.Duration(0)
	why := ""
	for i, c := range cands {
		d, err := s.probeSource(ctx, c)
		if err != nil {
			if why == "" {
				why = fmt.Sprintf("%s 连不上（%v）", c.Name, err)
			}
			continue
		}
		if i == 0 || bestDur == 0 || d < bestDur {
			best, bestDur = c, d
			why = fmt.Sprintf("实测最快：%s 用时 %dms", c.Name, d.Milliseconds())
		}
	}
	if why == "" {
		why = "两个 npm 源都没测通，先用默认源"
	}

	marketCacheState.mu.Lock()
	marketCacheState.npmPick, marketCacheState.npmAt, marketCacheState.npmWhy = best.ID, time.Now(), why
	marketCacheState.mu.Unlock()
	return best, why
}

func (s *Server) probeSource(ctx context.Context, src market.Source) (time.Duration, error) {
	start := time.Now()
	raw := strings.TrimSuffix(src.BaseURL, "/") + "/left-pad"
	cctx, cancel := context.WithTimeout(ctx, marketProbeTimeout)
	defer cancel()
	req, err := http.NewRequestWithContext(cctx, http.MethodGet, raw, nil)
	if err != nil {
		return 0, err
	}
	req.Header.Set("User-Agent", "gleam/"+buildinfo.Version)
	resp, err := (&http.Client{Timeout: marketProbeTimeout}).Do(req)
	if err != nil {
		return 0, err
	}
	defer resp.Body.Close()
	n, _ := io.Copy(io.Discard, io.LimitReader(resp.Body, 64<<10))
	s.recordMarketEgress(raw, n)
	if resp.StatusCode != http.StatusOK {
		return 0, fmt.Errorf("HTTP %d", resp.StatusCode)
	}
	return time.Since(start), nil
}

func (s *Server) lookupRemotePreset(ctx context.Context, id string) (market.RemotePreset, string, bool) {
	marketCacheState.mu.Lock()
	for _, e := range marketCacheState.byQuery {
		for _, p := range e.presets {
			if p.ID == id {
				marketCacheState.mu.Unlock()
				return p, "", true
			}
		}
	}
	marketCacheState.mu.Unlock()

	key := id
	if i := strings.LastIndexAny(key, "/"); i >= 0 {
		key = key[i+1:]
	}
	presets, note := s.marketSearch(ctx, key, false)
	for _, p := range presets {
		if p.ID == id {
			return p, note, true
		}
	}
	return market.RemotePreset{}, note, false
}

func (s *Server) marketSearch(ctx context.Context, q string, force bool) ([]market.RemotePreset, string) {
	q = strings.TrimSpace(q)

	marketCacheState.mu.Lock()
	if !force && marketCacheState.byQuery != nil {
		if e, ok := marketCacheState.byQuery[q]; ok && time.Since(e.at) < marketCacheTTL {
			out := e.presets
			marketCacheState.mu.Unlock()
			return out, ""
		}
	}
	marketCacheState.mu.Unlock()

	var src market.Source
	for _, c := range market.DefaultSources() {
		if c.Kind == "mcp-registry" {
			src = c
		}
	}
	if src.BaseURL == "" {
		return nil, "没有配置目录源"
	}
	raw := src.BaseURL + "?limit=100"
	if q != "" {
		raw = src.BaseURL + "?search=" + url.QueryEscape(q) + "&limit=50"
	}

	body, err := s.fetchMarketURL(ctx, raw)
	if err != nil {
		cached, cErr := s.readMarketCache()
		if cErr == nil && len(cached) > 0 {
			out := market.FilterRemote(cached, q)
			return out, "目录源连不上（" + err.Error() + "），这一份来自上次缓存的浏览页，可能不全。"
		}
		return nil, "目录源连不上：" + err.Error()
	}
	presets, perr := market.ParseRegistry(body, src.ID)
	if perr != nil {
		return nil, "目录源返回的内容读不懂：" + perr.Error()
	}
	s.cacheMarketQuery(q, presets)
	if q == "" {
		s.writeMarketCache(body)
	}
	return presets, ""
}

func (s *Server) cacheMarketQuery(q string, presets []market.RemotePreset) {
	marketCacheState.mu.Lock()
	defer marketCacheState.mu.Unlock()
	if marketCacheState.byQuery == nil {
		marketCacheState.byQuery = map[string]marketEntry{}
	}
	marketCacheState.byQuery[q] = marketEntry{presets: presets, at: time.Now()}
	if len(marketCacheState.byQuery) <= marketQueryCacheMax {
		return
	}
	now := time.Now()
	for k, e := range marketCacheState.byQuery {
		if k != q && now.Sub(e.at) >= marketCacheTTL {
			delete(marketCacheState.byQuery, k)
		}
	}
	for len(marketCacheState.byQuery) > marketQueryCacheMax {
		oldest := ""
		var oldestAt time.Time
		for k, e := range marketCacheState.byQuery {
			if k == q {
				continue
			}
			if oldest == "" || e.at.Before(oldestAt) {
				oldest, oldestAt = k, e.at
			}
		}
		if oldest == "" {
			break
		}
		delete(marketCacheState.byQuery, oldest)
	}
}

func (s *Server) marketCachePath() string {
	dir := strings.TrimSpace(s.Agent.DataDir())
	if dir == "" {
		return ""
	}
	return filepath.Join(dir, marketCacheName)
}

func (s *Server) writeMarketCache(body []byte) {
	p := s.marketCachePath()
	if p == "" {
		return
	}
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		return
	}
	_ = atomicfile.Write(p, body, 0o600)
}

func (s *Server) readMarketCache() ([]market.RemotePreset, error) {
	p := s.marketCachePath()
	if p == "" {
		return nil, fmt.Errorf("没有数据目录")
	}
	body, err := os.ReadFile(p)
	if err != nil {
		return nil, err
	}
	return market.ParseRegistry(body, "official-mcp")
}

func (s *Server) fetchMarketURL(ctx context.Context, raw string) ([]byte, error) {
	cctx, cancel := context.WithTimeout(ctx, marketFetchTimeout)
	defer cancel()
	req, err := http.NewRequestWithContext(cctx, http.MethodGet, raw, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", "gleam/"+buildinfo.Version)
	req.Header.Set("Accept", "application/json")
	resp, err := (&http.Client{Timeout: marketFetchTimeout}).Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, marketMaxBody))
	s.recordMarketEgress(raw, int64(len(body)))
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("HTTP %d", resp.StatusCode)
	}
	return body, nil
}

func (s *Server) recordMarketEgress(raw string, n int64) {
	if s.Agent == nil || s.Agent.Gate == nil {
		return
	}
	host := raw
	if u, err := url.Parse(raw); err == nil && u.Host != "" {
		host = u.Host
	}
	s.Agent.Gate.RecordEgress("market.remote", host, int(n))
}

func marketInstallArgs(src market.Source, isNPM bool, args []string) []string {
	if !isNPM || src.Kind != "npm" || strings.Contains(src.BaseURL, "registry.npmjs.org") {
		return args
	}
	out := make([]string, 0, len(args)+1)
	out = append(out, "--registry="+src.BaseURL)
	return append(out, args...)
}

func (s *Server) handleMarketRuntimes(w http.ResponseWriter, _ *http.Request) {
	list := market.Runtimes()
	missing := []string{}
	for _, r := range list {
		if !r.Found {
			missing = append(missing, r.Name)
		}
	}
	writeJSON(w, 200, map[string]any{"runtimes": list, "missing": missing})
}
