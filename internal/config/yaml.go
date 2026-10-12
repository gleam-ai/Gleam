package config

import (
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"
	"unicode"
)

type yline struct {
	indent int
	text   string
	isDash bool
	num    int
}

type parser struct {
	lines []yline
	i     int
}

func ParseYAML(src string) (map[string]any, error) {
	lines, err := scanLines(src)
	if err != nil {
		return nil, err
	}
	if len(lines) == 0 {
		return map[string]any{}, nil
	}
	p := &parser{lines: lines}
	if lines[0].isDash {
		return nil, fmt.Errorf("yaml 第 %d 行: 根节点不支持列表", lines[0].num)
	}
	v, err := p.parseMap(lines[0].indent)
	if err != nil {
		return nil, err
	}
	if p.i < len(p.lines) {
		return nil, fmt.Errorf("yaml 第 %d 行: 意外的内容 %q", p.lines[p.i].num, p.lines[p.i].text)
	}
	m, ok := v.(map[string]any)
	if !ok {
		return nil, fmt.Errorf("yaml 根节点必须是映射")
	}
	return m, nil
}

func scanLines(src string) ([]yline, error) {
	var out []yline
	for n, raw := range strings.Split(src, "\n") {
		raw = strings.TrimRight(raw, "\r")
		trimmed := strings.TrimLeft(raw, " \t")
		lead := raw[:len(raw)-len(trimmed)]
		if strings.Contains(lead, "\t") {
			return nil, fmt.Errorf("yaml 第 %d 行: 不允许使用 Tab 缩进", n+1)
		}
		if trimmed == "" || strings.HasPrefix(trimmed, "#") {
			continue
		}
		text := strings.TrimRight(trimmed, " ")
		isDash := text == "-" || strings.HasPrefix(text, "- ")
		out = append(out, yline{indent: len(lead), text: text, isDash: isDash, num: n + 1})
	}
	return out, nil
}

func (p *parser) cur() *yline {
	if p.i < len(p.lines) {
		return &p.lines[p.i]
	}
	return nil
}

func (p *parser) parseMap(indent int) (any, error) {
	m := map[string]any{}
	for {
		ln := p.cur()
		if ln == nil || ln.indent < indent || ln.isDash && ln.indent == indent {
			break
		}
		if ln.indent > indent {
			return nil, fmt.Errorf("yaml 第 %d 行: 意外的缩进", ln.num)
		}
		key, val, ok := splitKV(ln.text)
		if !ok {
			return nil, fmt.Errorf("yaml 第 %d 行: 无法解析键值对 %q", ln.num, ln.text)
		}
		p.i++
		val = valueWithoutComment(val)
		if val != "" {
			sv, err := parseScalar(val, ln.num)
			if err != nil {
				return nil, err
			}
			m[key] = sv
			continue
		}
		next := p.cur()
		if next == nil {
			m[key] = nil
			continue
		}
		if next.indent > indent {
			v, err := p.parseBlock(next.indent)
			if err != nil {
				return nil, err
			}
			m[key] = v
		} else if next.indent == indent && next.isDash {
			v, err := p.parseList(indent)
			if err != nil {
				return nil, err
			}
			m[key] = v
		} else {
			m[key] = nil
		}
	}
	return m, nil
}

func (p *parser) parseBlock(indent int) (any, error) {
	ln := p.cur()
	if ln == nil {
		return nil, nil
	}
	if ln.isDash {
		return p.parseList(indent)
	}
	return p.parseMap(indent)
}

func (p *parser) parseList(indent int) (any, error) {
	var arr []any
	for {
		ln := p.cur()
		if ln == nil || ln.indent < indent {
			break
		}
		if ln.indent > indent {
			return nil, fmt.Errorf("yaml 第 %d 行: 意外的缩进", ln.num)
		}
		if !ln.isDash {
			break
		}
		rest := strings.TrimSpace(strings.TrimPrefix(ln.text, "-"))
		p.i++
		switch {
		case rest == "":
			next := p.cur()
			if next != nil && next.indent > indent {
				v, err := p.parseBlock(next.indent)
				if err != nil {
					return nil, err
				}
				arr = append(arr, v)
			} else {
				arr = append(arr, nil)
			}
		case isKVStart(rest):
			v, err := p.parseMapItem(indent, rest)
			if err != nil {
				return nil, err
			}
			arr = append(arr, v)
		default:
			sv, err := parseScalar(valueWithoutComment(rest), ln.num)
			if err != nil {
				return nil, err
			}
			arr = append(arr, sv)
		}
	}
	return arr, nil
}

func (p *parser) parseMapItem(dashIndent int, rest string) (any, error) {
	keyIndent := dashIndent + 2
	key, val, ok := splitKV(rest)
	if !ok {
		return nil, fmt.Errorf("yaml 第 %d 行: 无法解析列表项 %q", p.lines[p.i-1].num, rest)
	}
	m := map[string]any{}
	val = valueWithoutComment(val)
	if val != "" {
		sv, err := parseScalar(val, p.lines[p.i-1].num)
		if err != nil {
			return nil, err
		}
		m[key] = sv
	} else {
		next := p.cur()
		if next != nil && (next.indent > keyIndent || (next.indent == keyIndent && !next.isDash)) {
			if next.indent > keyIndent {
				v, err := p.parseBlock(next.indent)
				if err != nil {
					return nil, err
				}
				m[key] = v
			} else {
				m[key] = nil
			}
		} else if next != nil && next.indent == keyIndent && next.isDash {
			v, err := p.parseList(keyIndent)
			if err != nil {
				return nil, err
			}
			m[key] = v
		} else {
			m[key] = nil
		}
	}
	for {
		ln := p.cur()
		if ln == nil || ln.indent <= dashIndent || ln.isDash {
			break
		}
		if ln.indent != keyIndent {
			return nil, fmt.Errorf("yaml 第 %d 行: 意外的缩进", ln.num)
		}
		k, v, ok := splitKV(ln.text)
		if !ok {
			return nil, fmt.Errorf("yaml 第 %d 行: 无法解析键值对 %q", ln.num, ln.text)
		}
		p.i++
		v = valueWithoutComment(v)
		if v != "" {
			sv, err := parseScalar(v, ln.num)
			if err != nil {
				return nil, err
			}
			m[k] = sv
			continue
		}
		next := p.cur()
		if next == nil {
			m[k] = nil
			continue
		}
		if next.indent > keyIndent {
			bv, err := p.parseBlock(next.indent)
			if err != nil {
				return nil, err
			}
			m[k] = bv
		} else if next.indent == keyIndent && next.isDash {
			bv, err := p.parseList(keyIndent)
			if err != nil {
				return nil, err
			}
			m[k] = bv
		} else {
			m[k] = nil
		}
	}
	return m, nil
}

func isKVStart(s string) bool {
	_, _, ok := splitKV(s)
	return ok
}

func splitKV(s string) (key, val string, ok bool) {
	var quote byte
	for i := 0; i < len(s); i++ {
		c := s[i]
		switch {
		case quote != 0:
			if c == quote {
				quote = 0
			}
		case c == '"' || c == '\'':
			quote = c
		case c == ':':
			if i+1 >= len(s) || s[i+1] == ' ' {
				k := strings.TrimSpace(s[:i])
				if k == "" {
					return "", "", false
				}
				return unquoteKey(k), strings.TrimSpace(s[i+1:]), true
			}
		}
	}
	return "", "", false
}

func valueWithoutComment(val string) string {
	t := strings.TrimSpace(val)
	if t == "" {
		return ""
	}
	if strings.HasPrefix(t, "#") {
		return ""
	}
	if idx := findComment(t); idx >= 0 {
		return strings.TrimSpace(t[:idx])
	}
	return t
}

func findComment(s string) int {
	var quote byte
	depth := 0
	for i := 0; i < len(s); i++ {
		c := s[i]
		switch {
		case quote != 0:
			if c == '\\' && quote == '"' {
				i++
			} else if c == quote {
				quote = 0
			}
		case c == '"' || c == '\'':
			quote = c
		case c == '[' || c == '{':
			depth++
		case c == ']' || c == '}':
			depth--
		case c == '#':
			if i == 0 || s[i-1] == ' ' || s[i-1] == '\t' {
				return i
			}
		}
	}
	return -1
}

func unquoteKey(k string) string {
	if len(k) >= 2 && (k[0] == '"' && k[len(k)-1] == '"' || k[0] == '\'' && k[len(k)-1] == '\'') {
		if v, err := strconv.Unquote(k); err == nil {
			return v
		}
		return k[1 : len(k)-1]
	}
	return k
}

func parseScalar(s string, line int) (any, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return nil, nil
	}
	if s[0] == '"' {
		if len(s) < 2 || !strings.HasSuffix(s, `"`) {
			return nil, fmt.Errorf("yaml 第 %d 行: 引号未闭合", line)
		}
		if v, err := strconv.Unquote(s); err == nil {
			return v, nil
		}
		return s[1 : len(s)-1], nil
	}
	if s[0] == '\'' {
		if len(s) < 2 || !strings.HasSuffix(s, `'`) {
			return nil, fmt.Errorf("yaml 第 %d 行: 引号未闭合", line)
		}
		return strings.ReplaceAll(s[1:len(s)-1], "''", "'"), nil
	}
	if strings.HasPrefix(s, "[") && strings.HasSuffix(s, "]") {
		inner := strings.TrimSpace(s[1 : len(s)-1])
		if inner == "" {
			return []any{}, nil
		}
		parts, err := splitTop(inner, ',', line)
		if err != nil {
			return nil, err
		}
		arr := make([]any, 0, len(parts))
		for _, part := range parts {
			sv, err := parseScalar(part, line)
			if err != nil {
				return nil, err
			}
			arr = append(arr, sv)
		}
		return arr, nil
	}
	if strings.HasPrefix(s, "{") && strings.HasSuffix(s, "}") {
		inner := strings.TrimSpace(s[1 : len(s)-1])
		m := map[string]any{}
		if inner == "" {
			return m, nil
		}
		parts, err := splitTop(inner, ',', line)
		if err != nil {
			return nil, err
		}
		for _, part := range parts {
			k, v, ok := splitKV(strings.TrimSpace(part))
			if !ok {
				return nil, fmt.Errorf("yaml 第 %d 行: 无法解析内联映射项 %q", line, part)
			}
			sv, err := parseScalar(v, line)
			if err != nil {
				return nil, err
			}
			m[k] = sv
		}
		return m, nil
	}
	if idx := findComment(s); idx >= 0 {
		s = strings.TrimSpace(s[:idx])
		if s == "" {
			return nil, nil
		}
	}
	switch strings.ToLower(s) {
	case "true", "yes", "on":
		return true, nil
	case "false", "no", "off":
		return false, nil
	case "null", "~":
		return nil, nil
	}
	if i, err := strconv.ParseInt(s, 10, 64); err == nil {
		return i, nil
	}
	if f, err := strconv.ParseFloat(s, 64); err == nil {
		return f, nil
	}
	return s, nil
}

func splitTop(s string, sep byte, line int) ([]string, error) {
	var parts []string
	var quote byte
	depth := 0
	start := 0
	for i := 0; i < len(s); i++ {
		c := s[i]
		switch {
		case quote != 0:
			if c == quote {
				quote = 0
			}
		case c == '"' || c == '\'':
			quote = c
		case c == '[' || c == '{':
			depth++
		case c == ']' || c == '}':
			depth--
		case c == sep && depth == 0:
			parts = append(parts, strings.TrimSpace(s[start:i]))
			start = i + 1
		}
	}
	if depth != 0 || quote != 0 {
		return nil, fmt.Errorf("yaml 第 %d 行: 括号或引号未闭合", line)
	}
	parts = append(parts, strings.TrimSpace(s[start:]))
	return parts, nil
}

type YMap struct {
	keys []string
	vals map[string]any
}

func NewYMap() *YMap { return &YMap{vals: map[string]any{}} }

func (m *YMap) Set(key string, v any) *YMap {
	if _, exists := m.vals[key]; !exists {
		m.keys = append(m.keys, key)
	}
	m.vals[key] = v
	return m
}

func (m *YMap) Get(key string) (any, bool) {
	v, ok := m.vals[key]
	return v, ok
}

func MarshalYAML(v any) ([]byte, error) {
	var b strings.Builder
	switch t := v.(type) {
	case *YMap:
		if err := emitMap(&b, t, 0); err != nil {
			return nil, err
		}
	case map[string]any:
		ym := NewYMap()
		keys := make([]string, 0, len(t))
		for k := range t {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		for _, k := range keys {
			ym.Set(k, t[k])
		}
		if err := emitMap(&b, ym, 0); err != nil {
			return nil, err
		}
	default:
		return nil, fmt.Errorf("yaml 序列化仅支持映射根节点")
	}
	return []byte(b.String()), nil
}

func emitMap(b *strings.Builder, m *YMap, indent int) error {
	if len(m.keys) == 0 {
		b.WriteString(strings.Repeat(" ", indent) + "{}\n")
		return nil
	}
	pad := strings.Repeat(" ", indent)
	for _, k := range m.keys {
		v := m.vals[k]
		switch t := v.(type) {
		case *YMap:
			if len(t.keys) == 0 {
				b.WriteString(pad + quoteKey(k) + ": {}\n")
				continue
			}
			b.WriteString(pad + quoteKey(k) + ":\n")
			if err := emitMap(b, t, indent+2); err != nil {
				return err
			}
		case map[string]any:
			ym := NewYMap()
			keys := make([]string, 0, len(t))
			for kk := range t {
				keys = append(keys, kk)
			}
			sort.Strings(keys)
			for _, kk := range keys {
				ym.Set(kk, t[kk])
			}
			if len(ym.keys) == 0 {
				b.WriteString(pad + quoteKey(k) + ": {}\n")
				continue
			}
			b.WriteString(pad + quoteKey(k) + ":\n")
			if err := emitMap(b, ym, indent+2); err != nil {
				return err
			}
		case []any:
			if len(t) == 0 {
				b.WriteString(pad + quoteKey(k) + ": []\n")
				continue
			}
			b.WriteString(pad + quoteKey(k) + ":\n")
			if err := emitList(b, t, indent+2); err != nil {
				return err
			}
		default:
			s, err := scalarYAML(v)
			if err != nil {
				return err
			}
			b.WriteString(pad + quoteKey(k) + ": " + s + "\n")
		}
	}
	return nil
}

func emitList(b *strings.Builder, arr []any, indent int) error {
	pad := strings.Repeat(" ", indent)
	for _, item := range arr {
		switch t := item.(type) {
		case *YMap:
			if len(t.keys) == 0 {
				b.WriteString(pad + "- {}\n")
				continue
			}
			first := t.keys[0]
			fv := t.vals[first]
			switch ft := fv.(type) {
			case *YMap, []any:
				b.WriteString(pad + "- " + quoteKey(first) + ":\n")
				var err error
				switch ftt := ft.(type) {
				case *YMap:
					err = emitMap(b, ftt, indent+4)
				case []any:
					err = emitList(b, ftt, indent+4)
				}
				if err != nil {
					return err
				}
				sub := NewYMap()
				for _, k := range t.keys[1:] {
					sub.Set(k, t.vals[k])
				}
				if len(sub.keys) > 0 {
					var sb strings.Builder
					if err := emitMap(&sb, sub, indent+2); err != nil {
						return err
					}
					b.WriteString(sb.String())
				}
			default:
				s, err := scalarYAML(ft)
				if err != nil {
					return err
				}
				b.WriteString(pad + "- " + quoteKey(first) + ": " + s + "\n")
				sub := NewYMap()
				for _, k := range t.keys[1:] {
					sub.Set(k, t.vals[k])
				}
				if len(sub.keys) > 0 {
					var sb strings.Builder
					if err := emitMap(&sb, sub, indent+2); err != nil {
						return err
					}
					b.WriteString(sb.String())
				}
			}
		case []any:
			b.WriteString(pad + "-\n")
			if err := emitList(b, t, indent+2); err != nil {
				return err
			}
		case map[string]any:
			return fmt.Errorf("yaml 序列化: 不支持裸 map 列表项，请使用 YMap")
		default:
			s, err := scalarYAML(item)
			if err != nil {
				return err
			}
			b.WriteString(pad + "- " + s + "\n")
		}
	}
	return nil
}

func scalarYAML(v any) (string, error) {
	switch t := v.(type) {
	case nil:
		return "null", nil
	case bool:
		if t {
			return "true", nil
		}
		return "false", nil
	case int:
		return strconv.Itoa(t), nil
	case int64:
		return strconv.FormatInt(t, 10), nil
	case float64:
		return strconv.FormatFloat(t, 'g', -1, 64), nil
	case float32:
		return strconv.FormatFloat(float64(t), 'g', -1, 32), nil
	case string:
		return quoteStr(t), nil
	case time.Time:
		return quoteStr(t.String()), nil
	default:
		return "", fmt.Errorf("yaml 序列化: 不支持的类型 %T", v)
	}
}

func quoteKey(k string) string {
	if k == "" || needsQuote(k) {
		return strconv.Quote(k)
	}
	return k
}

func quoteStr(s string) string {
	if s == "" || needsQuote(s) {
		return strconv.Quote(s)
	}
	return s
}

func needsQuote(s string) bool {
	if strings.HasPrefix(s, " ") || strings.HasSuffix(s, " ") {
		return true
	}
	if strings.ContainsAny(s, ":#{}[],&*?|<>=!%@`\"'\\\n\t") {
		return true
	}
	if strings.HasPrefix(s, "-") || strings.HasPrefix(s, "[") || strings.HasPrefix(s, "{") {
		return true
	}
	low := strings.ToLower(s)
	switch low {
	case "true", "false", "null", "~", "yes", "no", "on", "off":
		return true
	}
	if _, err := strconv.ParseInt(s, 10, 64); err == nil {
		return true
	}
	if _, err := strconv.ParseFloat(s, 64); err == nil {
		return true
	}
	for _, r := range s {
		if !unicode.IsPrint(r) {
			return true
		}
	}
	return false
}
