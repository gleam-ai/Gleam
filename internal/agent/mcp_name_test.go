package agent

import (
	"strings"
	"testing"
)

func TestSanitizeMCPName(t *testing.T) {
	cases := []struct{ in, want string }{
		{"npx", "npx"},
		{"mcp-server-filesystem", "mcp-server-filesystem"},
		{"Server_FileSystem", "server_filesystem"},
		{"mcp server.fetch", "mcp-server-fetch"},
		{"中文服务", ""},
		{"---", ""},
		{"  spaced  ", "spaced"},
		{"a!!!!!!!!!!!!!!!b", "a-b"},
		{strings.Repeat("a", 40), strings.Repeat("a", 32)},
	}
	for _, c := range cases {
		got := sanitizeMCPName(c.in)
		if got != c.want {
			t.Errorf("sanitizeMCPName(%q) = %q，期望 %q", c.in, got, c.want)
		}
		if got != "" && !mcpNameRe.MatchString(got) {
			t.Errorf("sanitizeMCPName(%q) = %q，仍不是合法名", c.in, got)
		}
	}
}
