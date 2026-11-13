# Gleam v0.0.25 - Computer Use Automation Integration

**Release Date**: 2026-11-02  
**Version**: v0.0.25-cua-integration

## 🎯 What's New

### Computer Use Automation (CUA)

Gleam now **controls your desktop** through natural language, marking a transition from "planning executor" to true "desktop assistant".

#### Key Features

✅ **Natural Language Control**  
Commands like:
- `"打开 Chrome 访问 github.com"` → Opens browser automatically  
- `"在微信中发送消息给测试人员"` → Communicates via WeChat  
- `"比较 https://github.com/gleam-ai/Gleam 和 MCP"` → Detects URL and triggers comparison  

✅ **URL Auto-Detection**  
When URLs appear in tasks, Gleam automatically triggers computer use functions without additional configuration.

✅ **Windows UI Automation**  
Structured control recognition replaces coordinate-based clicking, supporting precise operations on Chinese applications.

✅ **IME Chinese Input Support**  
Optimized for WeChat, QQ, DingTalk with proper composition window handling.

✅ **Security by Default**  
- Blacklist mode blocking localhost/127.0.0.1  
- Command injection prevention (exec/shell blocked)  
- Full audit trail logging  
- Zero background processes (~45KB memory footprint)

#### Architecture

- **Adapter Layer**: `internal/agent/tools/cua/adapter.go` (200+ lines)
- **Router**: `internal/agent/taskrouter/*` (intelligent URL detection)
- **Safety**: `internal/harness/safety/cua_sandbox.go` (whitelist + audit)
- **Multi-platform**: Desktop Electron + VS Code extension support

## 📦 Download

| Platform | Format | Size | Link |
|----------|--------|------|------|
| Windows | NSIS Installer | ~127 MB | [Download](https://github.com/gleam-ai/Gleam/releases/latest/download/Gleam-Setup.exe) |
| macOS Intel | Binary | ~16 MB | [`gleam-darwin-amd64-v0.0.25`](gleam-darwin-amd64-v0.0.25) |
| macOS ARM64 | Binary | ~15 MB | [`gleam-darwin-arm64-v0.0.25`](gleam-darwin-arm64-v0.0.25) |
| Linux x64 | Binary | ~16 MB | [`gleam-linux-amd64-v0.0.25`](gleam-linux-amd64-v0.0.25) |
| Windows CLI | Single binary | ~16 MB | [`gleam-windows-amd64-v0.0.25.exe`](gleam-windows-amd64-v0.0.25.exe) |

## 🔧 Quick Start

```bash
# One-click installation (Windows)
# Download Gleam-Setup.exe and double-click

# CLI usage (all platforms)
./gleam app                                    # Desktop window with CUA
./gleam goal "打开 Chrome 并访问 github.com"    # Desktop automation!
./gleam goal "比较 https://github.com/gleam-ai/Gleam"  # URL auto-detect
```

## 🛡️ Security

- **Whitelist Apps**: WeChat, QQ, DingTalk, Enterprise WeChat, Microsoft Teams (8+ Chinese apps)
- **Command Filtering**: Blocks exec/shell and other dangerous operations
- **Audit Trail**: Complete operation logging to disk
- **Event Triggered**: No persistent background processes, fully on-demand

## 📚 Documentation

- [`COMPUTER_USE_INTEGRATION.md`](../reports/COMPUTER_USE_INTEGRATION.md) - Full integration report
- [`ONE-CLICK-RELEASE.md`](../reports/ONE-CLICK-RELEASE.md) - Multi-platform release guide  
- [`config/taskrouter.json.example`](../../config/taskrouter.json.example) - Configuration template

## 🐛 Known Issues

- Unsigned macOS binaries require `chmod +x` and system permissions on first run
- UI Automation may not work in some virtualization/remote desktop environments

## 🙏 Contributors

Thanks to all contributors who made this update possible:
- CUA adapter implementation
- Chinese app whitelist expansion
- Security sandbox design

---

**Tags**: `v0.0.25` `cua` `computer-use` `automation` `desktop-control` `chinese-apps` `2026-11-02`
