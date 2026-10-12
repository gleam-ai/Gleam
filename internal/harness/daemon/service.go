package daemon

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"

	"gleam/internal/atomicfile"
)

type ServiceState string

const (
	StateInstalled ServiceState = "installed"
	StateRunning   ServiceState = "running"
	StateStopped   ServiceState = "stopped"
	StateUnknown   ServiceState = "unknown"
)

type ServiceInfo struct {
	Name    string
	State   ServiceState
	Enabled bool
}

type Manager struct {
	name    string
	dataDir string
	binPath string
}

func New(name, dataDir, binPath string) *Manager {
	return &Manager{
		name:    name,
		dataDir: dataDir,
		binPath: binPath,
	}
}

func (m *Manager) Install() error {
	switch runtime.GOOS {
	case "windows":
		return m.installWindows()
	case "darwin":
		return m.installMacOS()
	case "linux":
		return m.installLinux()
	default:
		return fmt.Errorf("不支持的平台：%s", runtime.GOOS)
	}
}

func (m *Manager) Uninstall() error {
	switch runtime.GOOS {
	case "windows":
		return m.uninstallWindows()
	case "darwin":
		return m.uninstallMacOS()
	case "linux":
		return m.uninstallLinux()
	default:
		return fmt.Errorf("不支持的平台：%s", runtime.GOOS)
	}
}

func (m *Manager) Start() error {
	switch runtime.GOOS {
	case "windows":
		return m.startWindows()
	case "darwin":
		return m.startMacOS()
	case "linux":
		return m.startLinux()
	default:
		return fmt.Errorf("不支持的平台：%s", runtime.GOOS)
	}
}

func (m *Manager) Stop() error {
	switch runtime.GOOS {
	case "windows":
		return m.stopWindows()
	case "darwin":
		return m.stopMacOS()
	case "linux":
		return m.stopLinux()
	default:
		return fmt.Errorf("不支持的平台：%s", runtime.GOOS)
	}
}

func (m *Manager) Status() (*ServiceInfo, error) {
	switch runtime.GOOS {
	case "windows":
		return m.statusWindows()
	case "darwin":
		return m.statusMacOS()
	case "linux":
		return m.statusLinux()
	default:
		return nil, fmt.Errorf("不支持的平台：%s", runtime.GOOS)
	}
}

func (m *Manager) installWindows() error {
	args := []string{
		"create", m.name,
		"binPath=", fmt.Sprintf(`"%s" app --daemon --data-dir "%s"`, m.binPath, m.dataDir),
		"start=", "auto",
		"obj=", "LocalService",
		"DisplayName=", "Gleam Daemon",
	}
	cmd := exec.Command("sc", args...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("注册 Windows 服务失败：%w\n%s", err, string(out))
	}
	return nil
}

func (m *Manager) uninstallWindows() error {
	cmd := exec.Command("sc", "delete", m.name)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("卸载 Windows 服务失败：%w\n%s", err, string(out))
	}
	return nil
}

func (m *Manager) startWindows() error {
	cmd := exec.Command("sc", "start", m.name)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("启动服务失败：%w\n%s", err, string(out))
	}
	return nil
}

func (m *Manager) stopWindows() error {
	cmd := exec.Command("sc", "stop", m.name)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("停止服务失败：%w\n%s", err, string(out))
	}
	return nil
}

func (m *Manager) statusWindows() (*ServiceInfo, error) {
	cmd := exec.Command("sc", "query", m.name)
	out, err := cmd.CombinedOutput()
	if err != nil {
		if strings.Contains(string(out), "1060") {
			return &ServiceInfo{Name: m.name, State: StateUnknown, Enabled: false}, nil
		}
		return nil, fmt.Errorf("查询服务状态失败：%w\n%s", err, string(out))
	}

	info := &ServiceInfo{Name: m.name}
	output := string(out)

	if strings.Contains(output, "RUNNING") {
		info.State = StateRunning
	} else if strings.Contains(output, "STOPPED") {
		info.State = StateStopped
	} else {
		info.State = StateUnknown
	}

	if strings.Contains(output, "AUTO_START") {
		info.Enabled = true
	}

	return info, nil
}

func (m *Manager) installMacOS() error {
	home, err := os.UserHomeDir()
	if err != nil {
		return err
	}

	plistDir := filepath.Join(home, "Library", "LaunchAgents")
	if err := os.MkdirAll(plistDir, 0o755); err != nil {
		return err
	}

	plistPath := filepath.Join(plistDir, fmt.Sprintf("top.wangjn.gleam.%s.plist", m.name))
	plist := fmt.Sprintf(`<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
    <key>Label</key>
    <string>top.wangjn.gleam.%s</string>
    <key>ProgramArguments</key>
    <array>
        <string>%s</string>
        <string>app</string>
        <string>--daemon</string>
        <string>--data-dir</string>
        <string>%s</string>
    </array>
    <key>RunAtLoad</key>
    <true/>
    <key>KeepAlive</key>
    <true/>
    <key>StandardOutPath</key>
    <string>%s/logs/daemon.log</string>
    <key>StandardErrorPath</key>
    <string>%s/logs/daemon-error.log</string>
</dict>
</plist>`, m.name, m.binPath, m.dataDir, m.dataDir, m.dataDir)

	if err := atomicfile.Write(plistPath, []byte(plist), 0o644); err != nil {
		return fmt.Errorf("写入 plist 失败：%w", err)
	}

	cmd := exec.Command("launchctl", "load", plistPath)
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("加载 launchd 服务失败：%w\n%s", err, string(out))
	}

	return nil
}

func (m *Manager) uninstallMacOS() error {
	home, err := os.UserHomeDir()
	if err != nil {
		return err
	}

	plistPath := filepath.Join(home, "Library", "LaunchAgents", fmt.Sprintf("top.wangjn.gleam.%s.plist", m.name))

	cmd := exec.Command("launchctl", "unload", plistPath)
	_ = cmd.Run() // best effort: the service may never have been loaded

	if err := os.Remove(plistPath); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("删除 plist 失败：%w", err)
	}

	return nil
}

func (m *Manager) startMacOS() error {
	home, err := os.UserHomeDir()
	if err != nil {
		return err
	}

	plistPath := filepath.Join(home, "Library", "LaunchAgents", fmt.Sprintf("top.wangjn.gleam.%s.plist", m.name))
	cmd := exec.Command("launchctl", "load", plistPath)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("启动服务失败：%w\n%s", err, string(out))
	}
	return nil
}

func (m *Manager) stopMacOS() error {
	home, err := os.UserHomeDir()
	if err != nil {
		return err
	}

	plistPath := filepath.Join(home, "Library", "LaunchAgents", fmt.Sprintf("top.wangjn.gleam.%s.plist", m.name))
	cmd := exec.Command("launchctl", "unload", plistPath)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("停止服务失败：%w\n%s", err, string(out))
	}
	return nil
}

func (m *Manager) statusMacOS() (*ServiceInfo, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return nil, err
	}

	plistPath := filepath.Join(home, "Library", "LaunchAgents", fmt.Sprintf("top.wangjn.gleam.%s.plist", m.name))

	if _, err := os.Stat(plistPath); os.IsNotExist(err) {
		return &ServiceInfo{Name: m.name, State: StateUnknown, Enabled: false}, nil
	}

	cmd := exec.Command("launchctl", "list", fmt.Sprintf("top.wangjn.gleam.%s", m.name))
	out, err := cmd.CombinedOutput()
	if err != nil {
		return &ServiceInfo{Name: m.name, State: StateStopped, Enabled: true}, nil
	}

	_ = out
	return &ServiceInfo{Name: m.name, State: StateRunning, Enabled: true}, nil
}

func (m *Manager) installLinux() error {
	home, err := os.UserHomeDir()
	if err != nil {
		return err
	}

	unitDir := filepath.Join(home, ".config", "systemd", "user")
	if err := os.MkdirAll(unitDir, 0o755); err != nil {
		return err
	}

	unitPath := filepath.Join(unitDir, fmt.Sprintf("gleam-%s.service", m.name))
	unit := fmt.Sprintf(`[Unit]
Description=Gleam Daemon (%s)
After=network.target

[Service]
Type=simple
ExecStart=%s app --daemon --data-dir %s
Restart=on-failure
RestartSec=10

[Install]
WantedBy=default.target
`, m.name, m.binPath, m.dataDir)

	if err := atomicfile.Write(unitPath, []byte(unit), 0o644); err != nil {
		return fmt.Errorf("写入 systemd unit 失败：%w", err)
	}

	cmd := exec.Command("systemctl", "--user", "daemon-reload")
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("重新加载 systemd 失败：%w\n%s", err, string(out))
	}

	cmd = exec.Command("systemctl", "--user", "enable", fmt.Sprintf("gleam-%s.service", m.name))
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("启用服务失败：%w\n%s", err, string(out))
	}

	return nil
}

func (m *Manager) uninstallLinux() error {
	cmd := exec.Command("systemctl", "--user", "disable", fmt.Sprintf("gleam-%s.service", m.name))
	_ = cmd.Run()

	cmd = exec.Command("systemctl", "--user", "stop", fmt.Sprintf("gleam-%s.service", m.name))
	_ = cmd.Run()

	home, err := os.UserHomeDir()
	if err != nil {
		return err
	}

	unitPath := filepath.Join(home, ".config", "systemd", "user", fmt.Sprintf("gleam-%s.service", m.name))
	if err := os.Remove(unitPath); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("删除 unit 文件失败：%w", err)
	}

	cmd = exec.Command("systemctl", "--user", "daemon-reload")
	_ = cmd.Run()

	return nil
}

func (m *Manager) startLinux() error {
	cmd := exec.Command("systemctl", "--user", "start", fmt.Sprintf("gleam-%s.service", m.name))
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("启动服务失败：%w\n%s", err, string(out))
	}
	return nil
}

func (m *Manager) stopLinux() error {
	cmd := exec.Command("systemctl", "--user", "stop", fmt.Sprintf("gleam-%s.service", m.name))
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("停止服务失败：%w\n%s", err, string(out))
	}
	return nil
}

func (m *Manager) statusLinux() (*ServiceInfo, error) {
	cmd := exec.Command("systemctl", "--user", "is-active", fmt.Sprintf("gleam-%s.service", m.name))
	out, err := cmd.CombinedOutput()
	output := strings.TrimSpace(string(out))

	if err != nil {
		if output == "inactive" || output == "failed" {
			return &ServiceInfo{Name: m.name, State: StateStopped, Enabled: true}, nil
		}
		return &ServiceInfo{Name: m.name, State: StateUnknown, Enabled: false}, nil
	}

	if output == "active" {
		return &ServiceInfo{Name: m.name, State: StateRunning, Enabled: true}, nil
	}

	return &ServiceInfo{Name: m.name, State: StateUnknown, Enabled: false}, nil
}
