// Package daemon 实现 Gleam 守护进程：将调度器注册为系统服务，实现 24/7 后台运行。
//
// 支持平台：
//   - Windows：通过 sc.exe 注册为系统服务（当前用户身份）
//   - macOS：通过 launchd plist（~/Library/LaunchAgents/）
//   - Linux：通过 systemd unit（~/.config/systemd/user/）
//
// 设计原则：
//   - 单设备方案：用户只需在自己的电脑上开启"后台运行"
//   - 数据主权：任务队列持久化到本地 SQLite，不依赖第三方云
//   - 安全默认：后台模式仅执行只读任务，写操作需二次确认
package daemon

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
)

// ServiceState 服务状态。
type ServiceState string

const (
	StateInstalled ServiceState = "installed"
	StateRunning   ServiceState = "running"
	StateStopped   ServiceState = "stopped"
	StateUnknown   ServiceState = "unknown"
)

// ServiceInfo 服务信息。
type ServiceInfo struct {
	Name    string
	State   ServiceState
	Enabled bool // 是否开机自启
}

// Manager 守护进程管理器（平台相关）。
type Manager struct {
	name    string
	dataDir string
	binPath string
}

// New 创建守护进程管理器。
func New(name, dataDir, binPath string) *Manager {
	return &Manager{
		name:    name,
		dataDir: dataDir,
		binPath: binPath,
	}
}

// Install 注册系统服务。
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

// Uninstall 卸载系统服务。
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

// Start 启动服务。
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

// Stop 停止服务。
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

// Status 查询服务状态。
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

// ---------- Windows ----------

func (m *Manager) installWindows() error {
	// 使用 sc.exe 注册服务（需要管理员权限）
	// 服务以 LocalService 身份运行，避免 SYSTEM 权限过高
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
		// 服务不存在
		if strings.Contains(string(out), "1060") {
			return &ServiceInfo{Name: m.name, State: StateUnknown, Enabled: false}, nil
		}
		return nil, fmt.Errorf("查询服务状态失败：%w\n%s", err, string(out))
	}

	info := &ServiceInfo{Name: m.name}
	output := string(out)

	// 解析状态
	if strings.Contains(output, "RUNNING") {
		info.State = StateRunning
	} else if strings.Contains(output, "STOPPED") {
		info.State = StateStopped
	} else {
		info.State = StateUnknown
	}

	// 解析启动类型
	if strings.Contains(output, "AUTO_START") {
		info.Enabled = true
	}

	return info, nil
}

// ---------- macOS ----------

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

	if err := os.WriteFile(plistPath, []byte(plist), 0o644); err != nil {
		return fmt.Errorf("写入 plist 失败：%w", err)
	}

	// 加载 plist
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

	// 先卸载
	cmd := exec.Command("launchctl", "unload", plistPath)
	_ = cmd.Run() // 忽略错误（可能未加载）

	// 删除 plist
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

	// 检查 plist 是否存在
	if _, err := os.Stat(plistPath); os.IsNotExist(err) {
		return &ServiceInfo{Name: m.name, State: StateUnknown, Enabled: false}, nil
	}

	// 检查是否已加载
	cmd := exec.Command("launchctl", "list", fmt.Sprintf("top.wangjn.gleam.%s", m.name))
	out, err := cmd.CombinedOutput()
	if err != nil {
		// 未加载
		return &ServiceInfo{Name: m.name, State: StateStopped, Enabled: true}, nil
	}

	_ = out
	return &ServiceInfo{Name: m.name, State: StateRunning, Enabled: true}, nil
}

// ---------- Linux ----------

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

	if err := os.WriteFile(unitPath, []byte(unit), 0o644); err != nil {
		return fmt.Errorf("写入 systemd unit 失败：%w", err)
	}

	// 重新加载 systemd
	cmd := exec.Command("systemctl", "--user", "daemon-reload")
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("重新加载 systemd 失败：%w\n%s", err, string(out))
	}

	// 启用服务（开机自启）
	cmd = exec.Command("systemctl", "--user", "enable", fmt.Sprintf("gleam-%s.service", m.name))
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("启用服务失败：%w\n%s", err, string(out))
	}

	return nil
}

func (m *Manager) uninstallLinux() error {
	// 禁用并停止服务
	cmd := exec.Command("systemctl", "--user", "disable", fmt.Sprintf("gleam-%s.service", m.name))
	_ = cmd.Run()

	cmd = exec.Command("systemctl", "--user", "stop", fmt.Sprintf("gleam-%s.service", m.name))
	_ = cmd.Run()

	// 删除 unit 文件
	home, err := os.UserHomeDir()
	if err != nil {
		return err
	}

	unitPath := filepath.Join(home, ".config", "systemd", "user", fmt.Sprintf("gleam-%s.service", m.name))
	if err := os.Remove(unitPath); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("删除 unit 文件失败：%w", err)
	}

	// 重新加载 systemd
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
		// 服务不存在或未激活
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
