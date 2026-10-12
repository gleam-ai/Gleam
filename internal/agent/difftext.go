package agent

import (
	"strconv"
	"strings"
)

const (
	diffMaxSideLines = 2000
	diffCellBudget   = 400_000
	diffMaxLines     = 400
)

const (
	DiffCtx = "ctx"
	DiffAdd = "add"
	DiffDel = "del"
)

type DiffLine struct {
	Kind string `json:"kind"`
	Text string `json:"text"`
	No   int    `json:"no"`
}

type DiffResult struct {
	Lines     []DiffLine `json:"lines"`
	Added     int        `json:"added"`
	Deleted   int        `json:"deleted"`
	Truncated bool       `json:"truncated"`
	Note      string     `json:"note,omitempty"`
}

func splitLines(s string) []string {
	if s == "" {
		return nil
	}
	parts := strings.Split(strings.ReplaceAll(s, "\r\n", "\n"), "\n")
	if n := len(parts); n > 0 && parts[n-1] == "" {
		parts = parts[:n-1]
	}
	return parts
}

func DiffText(before, after string) DiffResult {
	a, b := splitLines(before), splitLines(after)
	head := 0
	for head < len(a) && head < len(b) && a[head] == b[head] {
		head++
	}
	tail := 0
	for tail < len(a)-head && tail < len(b)-head && a[len(a)-1-tail] == b[len(b)-1-tail] {
		tail++
	}
	midA, midB := a[head:len(a)-tail], b[head:len(b)-tail]

	if len(midA) == 0 && len(midB) == 0 {
		return DiffResult{Note: "前后内容逐行一致"}
	}
	res := DiffResult{}
	if len(midA) > diffMaxSideLines || len(midB) > diffMaxSideLines ||
		len(midA)*max(len(midB), 1) > diffCellBudget {
		res.Truncated = true
		res.Added, res.Deleted = len(midB), len(midA)
		res.Note = "改动规模超过行级对比上限，只给出增删行数"
		return res
	}
	for i := max(head-3, 0); i < head; i++ {
		res.Lines = append(res.Lines, DiffLine{Kind: DiffCtx, Text: a[i], No: i + 1})
	}
	res.Lines = append(res.Lines, lcsLines(midA, midB, head)...)
	for k := 0; k < min(tail, 3); k++ {
		j := len(b) - tail + k
		res.Lines = append(res.Lines, DiffLine{Kind: DiffCtx, Text: b[j], No: j + 1})
	}
	for _, l := range res.Lines {
		switch l.Kind {
		case DiffAdd:
			res.Added++
		case DiffDel:
			res.Deleted++
		}
	}
	if len(res.Lines) > diffMaxLines {
		res.Lines = clampLines(res.Lines, diffMaxLines)
		res.Truncated = true
		res.Note = "改动较多，只列出前 " + strconv.Itoa(diffMaxLines) + " 行"
	}
	return res
}

func lcsLines(a, b []string, offset int) []DiffLine {
	m, n := len(a), len(b)
	if m == 0 {
		out := make([]DiffLine, 0, n)
		for j := 0; j < n; j++ {
			out = append(out, DiffLine{Kind: DiffAdd, Text: b[j], No: offset + j + 1})
		}
		return out
	}
	if n == 0 {
		out := make([]DiffLine, 0, m)
		for i := 0; i < m; i++ {
			out = append(out, DiffLine{Kind: DiffDel, Text: a[i], No: offset + i + 1})
		}
		return out
	}
	table := make([]int32, (m+1)*(n+1))
	at := func(i, j int) *int32 { return &table[i*(n+1)+j] }
	for i := m - 1; i >= 0; i-- {
		for j := n - 1; j >= 0; j-- {
			if a[i] == b[j] {
				*at(i, j) = *at(i+1, j+1) + 1
			} else if x, y := *at(i+1, j), *at(i, j+1); x >= y {
				*at(i, j) = x
			} else {
				*at(i, j) = y
			}
		}
	}
	var out []DiffLine
	i, j := 0, 0
	for i < m && j < n {
		switch {
		case a[i] == b[j]:
			out = append(out, DiffLine{Kind: DiffCtx, Text: a[i], No: offset + i + 1})
			i, j = i+1, j+1
		case *at(i+1, j) >= *at(i, j+1):
			out = append(out, DiffLine{Kind: DiffDel, Text: a[i], No: offset + i + 1})
			i++
		default:
			out = append(out, DiffLine{Kind: DiffAdd, Text: b[j], No: offset + j + 1})
			j++
		}
	}
	for ; i < m; i++ {
		out = append(out, DiffLine{Kind: DiffDel, Text: a[i], No: offset + i + 1})
	}
	for ; j < n; j++ {
		out = append(out, DiffLine{Kind: DiffAdd, Text: b[j], No: offset + j + 1})
	}
	return out
}

func clampLines(lines []DiffLine, keep int) []DiffLine {
	out := append([]DiffLine(nil), lines[:keep]...)
	out = append(out, DiffLine{Kind: DiffCtx, Text: "…… 其余行省略", No: 0})
	return out
}
