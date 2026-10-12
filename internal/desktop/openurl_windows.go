//go:build windows

package desktop

import "os/exec"

func openURLPlatform(rawURL string) error {
	return exec.Command("rundll32", "url.dll,FileProtocolHandler", rawURL).Start()
}
