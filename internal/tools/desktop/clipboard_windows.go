//go:build windows

package desktop

import (
	"os/exec"
	"strings"
)

func clipboardRead() (string, error) {
	out, err := exec.Command("powershell", "-NoProfile", "-Command", "Get-Clipboard -Raw").Output()
	if err != nil {
		return "", err
	}
	return string(out), nil
}

func clipboardWrite(text string) error {
	encoded := encodeBase64(text)
	cmd := exec.Command("powershell", "-NoProfile", "-Command",
		"$t=[System.Text.Encoding]::UTF8.GetString([System.Convert]::FromBase64String('"+encoded+"')); Set-Clipboard -Value $t")
	return cmd.Run()
}

func encodeBase64(s string) string {
	const tbl = "ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789+/"
	data := []byte(s)
	var b strings.Builder
	for i := 0; i < len(data); i += 3 {
		end := i + 3
		if end > len(data) {
			end = len(data)
		}
		chunk := data[i:end]
		var n uint32
		for j := 0; j < 3; j++ {
			n <<= 8
			if j < len(chunk) {
				n |= uint32(chunk[j])
			}
		}
		b.WriteByte(tbl[(n>>18)&0x3F])
		b.WriteByte(tbl[(n>>12)&0x3F])
		if len(chunk) > 1 {
			b.WriteByte(tbl[(n>>6)&0x3F])
		} else {
			b.WriteByte('=')
		}
		if len(chunk) > 2 {
			b.WriteByte(tbl[n&0x3F])
		} else {
			b.WriteByte('=')
		}
	}
	return b.String()
}
