# Gleam + CUA (Computer Use Automation) Integration Guide

## Overview

This document describes how Gleam integrates computer use automation capabilities through the CUA (Computer Use Automation) library, enabling AI agents to interact with native desktop applications and browsers.

## Quick Start

### Prerequisites

1. **Install CUA** (Python):
```bash
pip install cua-automation
export GLEAM_CUA_BINARY=$(which cua)
```

2. **Verify Installation**:
```bash
gleam doctor --computer-use
```

3. **Enable in Configuration**:
```json
{
  "gleam.computerUse.enabled": true,
  "gleam.computerUse.safetyLevel": "standard" // or "strict", "relaxed"
}
```

## Architecture

```
┌──────────────────────────────────────────────┐
│              Gleam Agent Core                │
├──────────────────────────────────────────────┤
│  Tools Registry → ComputerUse Tool           │
├──────────────────────────────────────────────┤
│     Safety Gate (whitelist, audit log)       │
├──────────────────────────────────────────────┤
│  Desktop Process → IPC → CUA Server          │
└──────────────────────────────────────────────┘
         ↓
   CUA Library (Rust core / Python SDK)
         ↓
   Native Apps / Browsers
```

## Usage Examples

### 1. Screenshot Analysis Flow

```go
// Agent automatically plans screenshot → analysis → action sequence
agent.Run("Take a screenshot of Chrome browser")

// Internally:
1. TakeScreenshot() → base64 image
2. Send to LLM for analysis
3. LLM returns element coordinates
4. Execute click/type based on coordinates
```

### 2. Application Automation

```bash
gleam run --task=open_browser_and_search --args='Google "Gleam AI"'

# Agent will:
1. Launch Chrome
2. Navigate to google.com  
3. Type query in search box
4. Submit form
5. Capture results
```

### 3. Custom Task via MCP

```python
import httpx

# Direct CUA MCP communication
response = httpx.post(
    'http://localhost:8796/rpc',
    json={
        'jsonrpc': '2.0',
        'id': 1,
        'method': 'cua:screenshot',
        'params': {'format': 'base64'}
    }
)

screenshot_data = response.json()['result']['data']
```

## Safety Features

### Application Whitelist

CUA integration maintains an application whitelist:

| Status | Applications |
|--------|--------------|
| ✅ Allowed | chrome, firefox, safari, vscode, terminal |
| ⚠️ Blocked | unknown executables |
| 🛑 Critical | shell commands, sudo, rm -rf |

### Action Audit Log

All actions are logged:

```bash
gleam history --computer-use
```

Sample output:
```json
[
  {"timestamp": "2026-10-28T14:30:00Z", "action": "click", "safe": true},
  {"timestamp": "2026-10-28T14:30:05Z", "action": "type_text", "safe": true},
]
```

## VS Code Extension Commands

| Command | Description |
|---------|-------------|
| `computer-use.screenshot` | Capture current screen |
| `computer-use.click x y` | Click at coordinates |
| `computer-use.type text` | Type into active element |
| `computer-use.open_app name` | Launch application |
| `computer-use.history` | View action log |

## Testing

### Run Unit Tests

```bash
cd internal/agent/tools/cua
go test -v -count=1
```

### E2E Test

```bash
gleam eval --layer smoke --computer-use
```

### Mock Mode Testing

When CUA binary is unavailable, operations run in mock mode:

```log
MOCK CLICK: 1920, 1080
MOCK TYPE: "hello world"
```

No actual system changes occur in mock mode.

## Troubleshooting

### Issue: "Computer Use not available"

**Solution**: 
1. Verify CUA installation: `cua --version`
2. Set environment variable: `export GLEAM_CUA_BINARY=/path/to/cua`
3. Check permissions (especially on macOS): System Preferences → Security & Privacy

### Issue: Actions blocked by safety gate

**Solution**: Add app to whitelist:
```bash
gleam config set computer_use.app_whitelist "myapp,anotherapp"
```

### Issue: Timeout errors

**Solution**: Increase timeout:
```go
// In code
cu.timeout = 60 * time.Second
```

## Advanced Configuration

### Performance Tuning

For faster screenshot capture:
```python
# cua_config.ini
[screenshot]
quality = medium  # fast | medium | high
format = webp     # png | jpeg | webp
```

### Custom Actions

Register additional CUA actions:
```go
tools.Register("cua:drag", func(ctx context.Context, args map[string]any) (any, error) {
    // Implement drag-and-drop logic
})
```

## Known Limitations

1. **Cross-platform support varies**: Windows > macOS > Linux
2. **Screen resolution dependency**: Coordinates work best at native resolution
3. **Virtual machines**: May require special configuration
4. **High-DPI displays**: Scaling factors need adjustment

## Future Roadmap

- [ ] Remote desktop support (SSH tunneling)
- [ ] Video stream integration
- [ ] OCR for non-text elements
- [ ] Keyboard shortcut macros
- [ ] File system automation
- [ ] Clipboard synchronization

## License Notes

CUA uses mixed licensing (MIT for core, FSL-1.1-MIT for Spaces). Ensure compliance before commercial deployment.

---

**Last Updated**: 2026-10-29
**Version**: v0.0.24
