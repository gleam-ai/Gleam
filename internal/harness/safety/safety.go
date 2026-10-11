package safety

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"gleam/internal/tools/toolutil"
	"gleam/pkg/types"
)

type Gate struct {
	mu              sync.RWMutex
	Mode            string
	TrustedTools    map[string]bool
	TrustedPaths    []string
	WorkspaceRoots  []string
	ApprovalTimeout time.Duration
	toolPerm        map[string]types.Permission
	auditMu         sync.Mutex
	audit           []AuditEntry
	auditPath       string
	pendingMu       sync.Mutex
	pending         []PendingApproval
	pendingPath     string
}

func New(mode string, trustedTools, trustedPaths, workspaceRoots []string, approvalTimeout time.Duration) *Gate {
	if mode == "" {
		mode = "auto"
	}
	if approvalTimeout <= 0 {
		approvalTimeout = 5 * time.Minute
	}
	tt := map[string]bool{}
	for _, t := range trustedTools {
		tt[t] = true
	}
	var tp []string
	addPath := func(p string) {
		if p == "" {
			return
		}
		if abs, err := filepath.Abs(p); err == nil {
			tp = append(tp, filepath.Clean(abs))
		} else {
			tp = append(tp, filepath.Clean(p))
		}
	}
	for _, p := range trustedPaths {
		addPath(p)
	}
	for _, p := range workspaceRoots {
		addPath(p)
	}
	return &Gate{
		Mode:            mode,
		TrustedTools:    tt,
		TrustedPaths:    tp,
		WorkspaceRoots:  workspaceRoots,
		ApprovalTimeout: approvalTimeout,
		toolPerm:        map[string]types.Permission{},
	}
}

type Decision struct {
	NeedApproval bool
	Risk         string
	Reason       string
}

type AuditEntry struct {
	Time        time.Time `json:"time"`
	Tool        string    `json:"tool"`
	Risk        string    `json:"risk"`
	Action      string    `json:"action"`
	Reason      string    `json:"reason"`
	Detail      string    `json:"detail,omitempty"`
	Egress      string    `json:"egress,omitempty"`
	EgressBytes int       `json:"egress_bytes,omitempty"`
}

const auditCap = 200

func (g *Gate) Evaluate(t types.Tool, args map[string]any) Decision {
	return g.evaluate(context.Background(), t, args, false)
}

func (g *Gate) EvaluateStep(t types.Tool, args map[string]any, preApproved bool) Decision {
	return g.evaluate(context.Background(), t, args, preApproved)
}

func (g *Gate) EvaluateStepIn(ctx context.Context, t types.Tool, args map[string]any, preApproved bool) Decision {
	return g.evaluate(ctx, t, args, preApproved)
}

func (g *Gate) evaluate(ctx context.Context, t types.Tool, args map[string]any, preApproved bool) Decision {
	perm := g.effectivePermission(t)
	switch perm {
	case types.PermissionReadOnly:
		return Decision{NeedApproval: false, Risk: "low", Reason: "只读操作"}
	}

	g.mu.RLock()
	trustedTool := g.TrustedTools[t.Name()]
	mode := g.Mode
	g.mu.RUnlock()

	risk := "medium"
	if perm == types.PermissionFullAccess {
		risk = "high"
	}

	if risk == "high" {
		return Decision{NeedApproval: true, Risk: risk, Reason: "高风险操作，必须人工确认"}
	}

	if trustedTool {
		return Decision{NeedApproval: false, Risk: risk, Reason: fmt.Sprintf("工具 %s 在信任白名单中", t.Name())}
	}

	if preApproved {
		return Decision{NeedApproval: false, Risk: risk, Reason: "计划已获用户批准"}
	}

	if mode == "plan_first" || mode == "interactive" {
		return Decision{NeedApproval: true, Risk: risk, Reason: fmt.Sprintf("%s 模式下所有写操作都需要确认", mode)}
	}

	if pa, ok := t.(types.PathAware); ok {
		paths := pa.Paths(ctx, args)
		if len(paths) == 0 {
			return Decision{NeedApproval: true, Risk: risk, Reason: "无法确认操作路径，需要人工确认"}
		}
		roots := toolutil.RootsFrom(ctx, nil)
		for _, p := range paths {
			if !g.pathTrusted(p) && !pathInRoots(roots, p) {
				return Decision{NeedApproval: true, Risk: risk, Reason: fmt.Sprintf("路径 %s 不在信任路径内", p)}
			}
		}
		return Decision{NeedApproval: false, Risk: risk, Reason: "操作路径均在信任路径内"}
	}
	return Decision{NeedApproval: true, Risk: risk, Reason: "无法确认操作范围，需要人工确认"}
}

func (g *Gate) pathTrusted(p string) bool {
	if p == "" {
		return false
	}
	if !filepath.IsAbs(p) {
		if len(g.WorkspaceRoots) > 0 {
			return true
		}
		return false
	}
	g.mu.RLock()
	defer g.mu.RUnlock()
	for _, root := range g.TrustedPaths {
		if pathWithin(root, p) {
			return true
		}
	}
	return false
}

func pathInRoots(roots []string, path string) bool {
	if path == "" || !filepath.IsAbs(path) {
		return false
	}
	for _, root := range roots {
		if strings.TrimSpace(root) == "" {
			continue
		}
		rel, err := filepath.Rel(filepath.Clean(root), filepath.Clean(path))
		if err != nil {
			continue
		}
		if rel == "." || (!strings.HasPrefix(rel, "..") && !filepath.IsAbs(rel)) {
			return true
		}
	}
	return false
}

func pathWithin(root, path string) bool {
	root = filepath.Clean(root)
	rel, err := filepath.Rel(root, path)
	if err != nil {
		return false
	}
	return rel == "." || (!strings.HasPrefix(rel, "..") && !filepath.IsAbs(rel))
}

func (g *Gate) DescribePaths(ctx context.Context, t types.Tool, args map[string]any) []string {
	if pa, ok := t.(types.PathAware); ok {
		return pa.Paths(ctx, args)
	}
	return nil
}

func (g *Gate) SetMode(mode string) {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.Mode = mode
}

func (g *Gate) SetApprovalTimeout(d time.Duration) {
	if d <= 0 {
		return
	}
	g.mu.Lock()
	defer g.mu.Unlock()
	g.ApprovalTimeout = d
}

func (g *Gate) SetWorkspaceRoots(roots []string) {
	g.mu.Lock()
	defer g.mu.Unlock()
	old := map[string]bool{}
	for _, p := range g.WorkspaceRoots {
		old[filepath.Clean(p)] = true
	}
	kept := g.TrustedPaths[:0]
	for _, p := range g.TrustedPaths {
		if !old[filepath.Clean(p)] {
			kept = append(kept, p)
		}
	}
	g.WorkspaceRoots = append([]string(nil), roots...)
	g.TrustedPaths = append(kept, roots...)
}

func (g *Gate) ApprovalTimeoutDuration() time.Duration {
	g.mu.RLock()
	defer g.mu.RUnlock()
	return g.ApprovalTimeout
}

func (g *Gate) SetToolPermission(name string, perm types.Permission) {
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.toolPerm == nil {
		g.toolPerm = map[string]types.Permission{}
	}
	g.toolPerm[name] = perm
}

func (g *Gate) ClearToolPermission(name string) {
	g.mu.Lock()
	defer g.mu.Unlock()
	delete(g.toolPerm, name)
}

func (g *Gate) SetToolPermissions(perms map[string]types.Permission) {
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.toolPerm == nil {
		g.toolPerm = map[string]types.Permission{}
	}
	for name, perm := range perms {
		g.toolPerm[name] = perm
	}
}

func (g *Gate) OverrideOf(name string) (types.Permission, bool) {
	g.mu.RLock()
	defer g.mu.RUnlock()
	p, ok := g.toolPerm[name]
	return p, ok
}

func (g *Gate) SetAuditPath(p string) {
	if g == nil {
		return
	}
	g.auditMu.Lock()
	g.auditPath = p
	g.auditMu.Unlock()
}

func (g *Gate) AuditPath() string {
	if g == nil {
		return ""
	}
	g.auditMu.Lock()
	defer g.auditMu.Unlock()
	return g.auditPath
}

func (g *Gate) Record(e AuditEntry) {
	if g == nil {
		return
	}
	if e.Time.IsZero() {
		e.Time = time.Now()
	}
	g.auditMu.Lock()
	g.audit = append(g.audit, e)
	if len(g.audit) > auditCap {
		g.audit = append([]AuditEntry(nil), g.audit[len(g.audit)-auditCap:]...)
	}
	path := g.auditPath
	g.auditMu.Unlock()

	if path != "" {
		g.appendAuditLine(path, e)
	}
}

func (g *Gate) RecordEgress(kind, host string, nbytes int) {
	if g == nil || host == "" || kind == "" {
		return
	}
	g.Record(AuditEntry{
		Tool:        kind,
		Action:      "egress",
		Reason:      fmt.Sprintf("数据出网：%s（%d 字节）", host, nbytes),
		Egress:      kind + ":" + host,
		EgressBytes: nbytes,
	})
}

func (g *Gate) appendAuditLine(path string, e AuditEntry) {
	data, err := json.Marshal(e)
	if err != nil {
		return
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return
	}
	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return
	}
	defer f.Close()
	_, _ = f.Write(append(data, '\n'))
}

func LoadAuditLog(path string, n int) ([]AuditEntry, error) {
	if path == "" {
		return nil, nil
	}
	f, err := os.Open(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	defer f.Close()
	var out []AuditEntry
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" {
			continue
		}
		var e AuditEntry
		if json.Unmarshal([]byte(line), &e) == nil {
			out = append(out, e)
		}
	}
	for i, j := 0, len(out)-1; i < j; i, j = i+1, j-1 {
		out[i], out[j] = out[j], out[i]
	}
	if n > 0 && len(out) > n {
		out = out[:n]
	}
	return out, nil
}

func (g *Gate) RecentAudit(n int) []AuditEntry {
	if g == nil || n <= 0 {
		return nil
	}
	g.auditMu.Lock()
	defer g.auditMu.Unlock()
	if n > len(g.audit) {
		n = len(g.audit)
	}
	out := make([]AuditEntry, 0, n)
	for i := len(g.audit) - 1; i >= len(g.audit)-n; i-- {
		out = append(out, g.audit[i])
	}
	return out
}

type EgressStat struct {
	Kind  string   `json:"kind"`
	Count int      `json:"count"`
	Hosts []string `json:"hosts"`
	Bytes int      `json:"bytes"`
}

type EgressReport struct {
	Stats   []EgressStat `json:"stats"`
	Scanned int          `json:"scanned"`
	Cap     int          `json:"cap"`
}

func (g *Gate) EgressStats() EgressReport {
	rep := EgressReport{Cap: auditCap, Stats: []EgressStat{}}
	if g == nil {
		return rep
	}
	g.auditMu.Lock()
	defer g.auditMu.Unlock()
	rep.Scanned = len(g.audit)
	byKind := map[string]*EgressStat{}
	seen := map[string]map[string]bool{}
	for _, e := range g.audit {
		if e.Action != "egress" {
			continue
		}
		st, ok := byKind[e.Tool]
		if !ok {
			st = &EgressStat{Kind: e.Tool, Hosts: []string{}}
			byKind[e.Tool] = st
			seen[e.Tool] = map[string]bool{}
		}
		st.Count++
		st.Bytes += e.EgressBytes
		if host := strings.TrimPrefix(e.Egress, e.Tool+":"); host != "" && host != e.Egress {
			if !seen[e.Tool][host] {
				seen[e.Tool][host] = true
				st.Hosts = append(st.Hosts, host)
			}
		}
	}
	for _, st := range byKind {
		sort.Strings(st.Hosts)
		rep.Stats = append(rep.Stats, *st)
	}
	sort.Slice(rep.Stats, func(i, j int) bool { return rep.Stats[i].Kind < rep.Stats[j].Kind })
	return rep
}

func (g *Gate) EffectivePermission(t types.Tool) types.Permission {
	return g.effectivePermission(t)
}

func (g *Gate) effectivePermission(t types.Tool) types.Permission {
	g.mu.RLock()
	defer g.mu.RUnlock()
	if p, ok := g.toolPerm[t.Name()]; ok {
		return p
	}
	return t.Permission()
}
