package credentials

import (
	"encoding/base64"
	"errors"
	"os/exec"
	"runtime"
	"strings"
)

// OS keyring access via the platform CLI, so no new module dependency is added.
// macOS uses `security` (Keychain); Linux uses `secret-tool` (secret-service /
// gnome-keyring). The master key is stored base64-encoded under a fixed
// service/account pair. Windows never calls these (DPAPI path), and the binary
// presence check below returns false there, driving the key-file fallback in CI.
const (
	keyringService = "gleam-credentials"
	keyringAccount = "master-key"
)

var errKeyringUnavailable = errors.New("credentials: 系统钥匙串不可用")

func keyringAvailable() bool {
	switch runtime.GOOS {
	case "darwin":
		_, err := exec.LookPath("security")
		return err == nil
	case "linux":
		_, err := exec.LookPath("secret-tool")
		return err == nil
	default:
		return false
	}
}

func keyringLoad() ([]byte, error) {
	var out []byte
	var err error
	switch runtime.GOOS {
	case "darwin":
		out, err = exec.Command("security", "find-generic-password",
			"-s", keyringService, "-a", keyringAccount, "-w").Output()
	case "linux":
		out, err = exec.Command("secret-tool", "lookup",
			"service", keyringService, "account", keyringAccount).Output()
	default:
		return nil, errKeyringUnavailable
	}
	if err != nil {
		return nil, err
	}
	return base64.StdEncoding.DecodeString(strings.TrimSpace(string(out)))
}

func keyringStore(key []byte) error {
	enc := base64.StdEncoding.EncodeToString(key)
	switch runtime.GOOS {
	case "darwin":
		// `security` has no stdin mode; -U updates the item in place. The key is
		// visible in argv only to same-user processes for the instant it runs.
		return exec.Command("security", "add-generic-password",
			"-U", "-s", keyringService, "-a", keyringAccount, "-w", enc).Run()
	case "linux":
		cmd := exec.Command("secret-tool", "store", "--label=Gleam credentials",
			"service", keyringService, "account", keyringAccount)
		cmd.Stdin = strings.NewReader(enc)
		return cmd.Run()
	default:
		return errKeyringUnavailable
	}
}
