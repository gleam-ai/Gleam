package market

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"
)

type Source struct {
	ID      string `json:"id"`
	Name    string `json:"name"`
	Kind    string `json:"kind"`
	BaseURL string `json:"base_url"`
	Region  string `json:"region"`
	Note    string `json:"note,omitempty"`
}

func BuiltinSource() Source {
	return Source{
		ID: "builtin", Name: "内置目录", Kind: "builtin", Region: "local",
		Note: "随程序发布的常量目录：离线可用，永远排在最后兜底。",
	}
}

var (
	MarketCatalogURL = "https://registry.modelcontextprotocol.io/v0/servers"
	MarketNPMURL     = "https://registry.npmjs.org"
	MarketNPMCNURL   = "https://registry.npmmirror.com"
)

func DefaultSources() []Source {
	return []Source{
		{
			ID: "official-mcp", Name: "MCP 官方注册表", Kind: "mcp-registry",
			BaseURL: MarketCatalogURL, Region: "global",
			Note: "由 Model Context Protocol 官方维护的服务器目录；目前没有国内镜像。",
		},
		{
			ID: "npm", Name: "npm registry", Kind: "npm",
			BaseURL: MarketNPMURL, Region: "global",
			Note: "查 npm 包是否真实存在、最新版本是多少。",
		},
		{
			ID: "npm-cn", Name: "npm 国内镜像（npmmirror）", Kind: "npm",
			BaseURL: MarketNPMCNURL, Region: "cn",
			Note: "npm 的国内镜像；查包时用它，装包的命令仍由本机 npx 决定走哪个 registry。",
		},
		BuiltinSource(),
	}
}

type RemotePreset struct {
	ID          string         `json:"id"`
	Name        string         `json:"name"`
	Desc        string         `json:"desc"`
	Version     string         `json:"version,omitempty"`
	Command     string         `json:"command,omitempty"`
	BaseArgs    []string       `json:"base_args,omitempty"`
	Params      []Param        `json:"params,omitempty"`
	Trust       string         `json:"trust"`
	Tags        []string       `json:"tags,omitempty"`
	PkgRef      string         `json:"pkg_ref,omitempty"`
	Kind        string         `json:"kind,omitempty"`
	URL         string         `json:"url,omitempty"`
	Headers     []RemoteHeader `json:"headers,omitempty"`
	Source      string         `json:"source"`
	Homepage    string         `json:"homepage,omitempty"`
	Installable bool           `json:"installable"`
	Unsupported string         `json:"unsupported,omitempty"`
}

type registryResponse struct {
	Servers []struct {
		Server registryServer `json:"server"`
	} `json:"servers"`
}

type registryRemote struct {
	Type    string           `json:"type"`
	URL     string           `json:"url"`
	Headers []registryHeader `json:"headers"`
}

type registryHeader struct {
	Name        string `json:"name"`
	Value       string `json:"value"`
	IsRequired  bool   `json:"isRequired"`
	IsSecret    bool   `json:"isSecret"`
	Description string `json:"description"`
}

type RemoteHeader struct {
	Name        string `json:"name"`
	Value       string `json:"value"`
	Required    bool   `json:"required,omitempty"`
	Secret      bool   `json:"secret,omitempty"`
	Description string `json:"description,omitempty"`
}

type registryServer struct {
	Name        string            `json:"name"`
	Title       string            `json:"title"`
	Description string            `json:"description"`
	Version     string            `json:"version"`
	Repository  map[string]any    `json:"repository"`
	Packages    []registryPackage `json:"packages"`
	Remotes     []registryRemote  `json:"remotes"`
}

type registryPackage struct {
	RegistryType    string `json:"registryType"`
	RegistryBaseURL string `json:"registryBaseUrl"`
	Identifier      string `json:"identifier"`
	Version         string `json:"version"`
	RuntimeHint     string `json:"runtimeHint"`
	Transport       struct {
		Type string `json:"type"`
	} `json:"transport"`
	RuntimeArguments []struct {
		Value string `json:"value"`
		Type  string `json:"type"`
	} `json:"runtimeArguments"`
	EnvironmentVariables []struct {
		Name        string `json:"name"`
		Description string `json:"description"`
		IsRequired  bool   `json:"isRequired"`
	} `json:"environmentVariables"`
}

func runtimeFor(registryType, hint string) (string, bool) {
	switch strings.ToLower(strings.TrimSpace(registryType)) {
	case "npm":
		return "npx", true
	case "pypi":
		return "uvx", true
	}
	switch strings.ToLower(strings.TrimSpace(hint)) {
	case "npx", "uvx":
		return strings.ToLower(hint), true
	}
	return "", false
}

func ParseRegistry(body []byte, sourceID string) ([]RemotePreset, error) {
	var resp registryResponse
	if err := json.Unmarshal(body, &resp); err != nil {
		return nil, err
	}
	out := make([]RemotePreset, 0, len(resp.Servers))
	best := map[string]int{}
	for _, it := range resp.Servers {
		s := it.Server
		if strings.TrimSpace(s.Name) == "" {
			continue
		}
		p := RemotePreset{
			ID: s.Name, Name: firstNonEmpty(s.Title, s.Name), Desc: s.Description,
			Version: s.Version, Source: sourceID, Trust: "user_approved",
		}
		if u, ok := s.Repository["url"].(string); ok {
			p.Homepage = u
		}
		pkg, hasPkg := pickStdioPackage(s.Packages)
		if !hasPkg {
			if rm, ok := pickStreamableRemote(s.Remotes); ok {
				p.Kind, p.URL, p.Installable = "http", rm.URL, true
				for _, h := range rm.Headers {
					if strings.TrimSpace(h.Name) == "" {
						continue
					}
					rh := RemoteHeader{Name: h.Name, Value: h.Value, Required: h.IsRequired, Secret: h.IsSecret, Description: h.Description}
					p.Headers = append(p.Headers, rh)
					for _, ph := range placeholders(h.Value) {
						p.Params = append(p.Params, Param{
							Key: ph, Label: headerLabel(h.Name, h.Description),
							Placeholder: h.Description, Required: h.IsRequired, Secret: h.IsSecret, Kind: "header",
						})
					}
				}
				out = append(out, p)
				continue
			}
		}
		cmd, cmdOK := runtimeFor(pkg.RegistryType, pkg.RuntimeHint)
		if !hasPkg || !cmdOK {
			switch {
			case !hasPkg:
				p.Unsupported = unsupportedReason(s)
			default:
				p.Unsupported = "这个包只有 " + pkg.RegistryType + " 分发方式，本机没有对应运行时"
			}
			out, best = emit(out, best, p)
			continue
		}
		ref := pkg.Identifier
		if pkg.Version != "" {
			ref += "@" + pkg.Version
		}
		args := []string{ref}
		for _, a := range pkg.RuntimeArguments {
			if strings.TrimSpace(a.Value) != "" {
				args = append([]string{a.Value}, args...)
			}
		}
		p.Command, p.BaseArgs, p.PkgRef, p.Installable = cmd, args, ref, true
		for _, ev := range pkg.EnvironmentVariables {
			if strings.TrimSpace(ev.Name) == "" {
				continue
			}
			p.Params = append(p.Params, Param{
				Key: ev.Name, Label: "环境变量 " + ev.Name, Placeholder: ev.Description,
				Required: ev.IsRequired, Kind: "env",
			})
		}
		out, best = emit(out, best, p)
	}
	return out, nil
}

func emit(out []RemotePreset, best map[string]int, p RemotePreset) ([]RemotePreset, map[string]int) {
	if idx, dup := best[p.ID]; dup {
		if compareVersion(p.Version, out[idx].Version) <= 0 {
			return out, best
		}
		out[idx] = p
		return out, best
	}
	best[p.ID] = len(out)
	return append(out, p), best
}

func pickStdioPackage(pkgs []registryPackage) (registryPackage, bool) {
	for _, want := range []string{"npm", "pypi"} {
		for _, p := range pkgs {
			if strings.EqualFold(p.RegistryType, want) && strings.TrimSpace(p.Identifier) != "" {
				return p, true
			}
		}
	}
	return registryPackage{}, false
}

func unsupportedReason(s registryServer) string {
	if len(s.Remotes) > 0 {
		kinds := map[string]bool{}
		for _, r := range s.Remotes {
			if r.Type != "" {
				kinds[r.Type] = true
			}
		}
		list := make([]string, 0, len(kinds))
		for k := range kinds {
			list = append(list, k)
		}
		sort.Strings(list)
		return "只提供远端接入（" + strings.Join(list, " / ") + "），本版实现了的是 streamable-http"
	}
	if len(s.Packages) > 0 {
		seen := map[string]bool{}
		kinds := make([]string, 0, len(s.Packages))
		for _, p := range s.Packages {
			if k := strings.TrimSpace(p.RegistryType); k != "" && !seen[k] {
				seen[k] = true
				kinds = append(kinds, k)
			}
		}
		sort.Strings(kinds)
		return "只有 " + strings.Join(kinds, " / ") + " 分发方式，本机没有对应运行时（只用 npx / uvx）"
	}
	return "这个条目没有提供任何可安装的分发方式"
}

func firstNonEmpty(vals ...string) string {
	for _, v := range vals {
		if strings.TrimSpace(v) != "" {
			return v
		}
	}
	return ""
}

func FilterRemote(in []RemotePreset, q string) []RemotePreset {
	q = strings.ToLower(strings.TrimSpace(q))
	if q == "" {
		return in
	}
	out := make([]RemotePreset, 0, len(in))
	for _, p := range in {
		if matchKeyword(q, p.ID, p.Name, p.Desc, strings.Join(p.Tags, " ")) {
			out = append(out, p)
		}
	}
	return out
}

func compareVersion(a, b string) int {
	pa, pb := versionSegs(a), versionSegs(b)
	for i := 0; i < len(pa) || i < len(pb); i++ {
		x, y := 0, 0
		if i < len(pa) {
			x = pa[i]
		}
		if i < len(pb) {
			y = pb[i]
		}
		if x != y {
			if x < y {
				return -1
			}
			return 1
		}
	}
	return 0
}

func versionSegs(v string) []int {
	v = strings.TrimPrefix(strings.TrimSpace(v), "v")
	if i := strings.IndexAny(v, "-+"); i >= 0 {
		v = v[:i]
	}
	parts := strings.Split(v, ".")
	out := make([]int, 0, len(parts))
	for _, p := range parts {
		n := 0
		if _, err := fmt.Sscanf(strings.TrimSpace(p), "%d", &n); err != nil {
			n = 0
		}
		out = append(out, n)
	}
	return out
}

func pickStreamableRemote(remotes []registryRemote) (registryRemote, bool) {
	for _, r := range remotes {
		if strings.EqualFold(strings.TrimSpace(r.Type), "streamable-http") && strings.TrimSpace(r.URL) != "" {
			return r, true
		}
	}
	return registryRemote{}, false
}

func placeholders(tmpl string) []string {
	var out []string
	seen := map[string]bool{}
	for i := 0; i < len(tmpl); i++ {
		if tmpl[i] != '{' {
			continue
		}
		j := strings.IndexByte(tmpl[i+1:], '}')
		if j < 0 {
			break
		}
		name := strings.TrimSpace(tmpl[i+1 : i+1+j])
		if name != "" && !seen[name] {
			seen[name] = true
			out = append(out, name)
		}
		i += j + 1
	}
	return out
}

func headerLabel(name, desc string) string {
	if strings.TrimSpace(desc) != "" {
		return desc
	}
	return "请求头 " + name
}

func BuildHeaders(p RemotePreset, values map[string]string) (map[string]string, error) {
	out := map[string]string{}
	for _, h := range p.Headers {
		val := h.Value
		for _, ph := range placeholders(h.Value) {
			v := strings.TrimSpace(values[ph])
			if v == "" && h.Required {
				return nil, fmt.Errorf("缺少必填项 %q（%s）", ph, headerLabel(h.Name, h.Description))
			}
			val = strings.ReplaceAll(val, "{"+ph+"}", v)
		}
		if strings.Contains(val, "{") && h.Required {
			return nil, fmt.Errorf("请求头 %s 的模板没填完：%s", h.Name, val)
		}
		if strings.TrimSpace(val) != "" {
			out[h.Name] = val
		}
	}
	return out, nil
}
