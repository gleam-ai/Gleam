# AI 硬件对接模块

Gleam 的 AI 硬件集成层，支持对接各类 AI 硬件设备（桌面伴侣、录音卡、传感器等）。

## 架构

```
internal/tools/hardware/
├── hardware.go      # 核心接口与注册表
── tools.go         # Gleam 工具暴露（hardware.list/send/receive 等）
── xiaohong.go      # AtomGit 小鸿驱动
└── qianwen_a2.go    # 千问办公录音卡 A2 驱动
```

## 已支持设备

| 设备 | ID | 能力 | 连接方式 |
|------|-----|------|----------|
| AtomGit 小鸿 | `xiaohong` | 语音、显示屏、按键、WiFi | HTTP API |
| 千问录音卡 A2 | `qianwen_a2` | 录音、转写、存储、USB | 串口通信 |

## Gleam 工具

| 工具 | 权限 | 说明 |
|------|------|------|
| `hardware.list` | 只读 | 列出所有注册设备 |
| `hardware.connect` | 需批准 | 连接到设备 |
| `hardware.disconnect` | 需批准 | 断开设备 |
| `hardware.send` | 需批准 | 发送命令到设备 |
| `hardware.receive` | 只读 | 从设备接收数据 |
| `hardware.info` | 只读 | 获取设备运行时信息 |

## HTTP API

| 端点 | 方法 | 说明 |
|------|------|------|
| `/api/hardware/devices` | GET | 列出注册设备 |
| `/api/hardware/instances` | GET | 列出已连接设备 |
| `/api/hardware/connect` | POST | 连接设备 |
| `/api/hardware/disconnect` | POST | 断开设备 |
| `/api/hardware/send` | POST | 发送命令 |
| `/api/hardware/receive` | POST | 接收数据 |
| `/api/hardware/info/{id}` | GET | 设备信息 |

## 添加新设备

### 1. 实现 Device 接口

```go
package hardware

import (
    "context"
    "sync"
)

type myDevice struct {
    mu    sync.RWMutex
    state DeviceState
}

func (d *myDevice) ID() string          { return "my_device" }
func (d *myDevice) Name() string        { return "我的设备" }
func (d *myDevice) Description() string { return "设备描述" }

func (d *myDevice) Capabilities() []Capability {
    return []Capability{CapAudioIn, CapSensor}
}

func (d *myDevice) State() DeviceState {
    d.mu.RLock()
    defer d.mu.RUnlock()
    return d.state
}

func (d *myDevice) Connect(ctx context.Context, config map[string]any) error {
    d.mu.Lock()
    defer d.mu.Unlock()
    // 初始化连接
    d.state = StateConnected
    return nil
}

func (d *myDevice) Disconnect(ctx context.Context) error {
    d.mu.Lock()
    defer d.mu.Unlock()
    d.state = StateDisconnected
    return nil
}

func (d *myDevice) Send(ctx context.Context, command string, payload any) (any, error) {
    // 处理命令
    return nil, nil
}

func (d *myDevice) Receive(ctx context.Context) (any, error) {
    // 读取数据
    return nil, nil
}

func (d *myDevice) Info(ctx context.Context) map[string]any {
    return map[string]any{"firmware": "1.0.0"}
}

func init() {
    Register(&myDevice{})
}
```

### 2. 能力类型

| 能力 | 说明 |
|------|------|
| `audio_in` | 麦克风/音频输入 |
| `audio_out` | 扬声器/音频输出 |
| `sensor` | 环境传感器 |
| `display` | 屏幕/LED 显示 |
| `button` | 物理按键 |
| `storage` | 本地存储 |
| `network` | WiFi/以太网 |
| `usb` | USB 主机/设备 |
| `ble` | 蓝牙低功耗 |
| `transcribe` | 本地语音转文字 |

### 3. 命令约定

设备命令使用字符串命名，payload 为任意 JSON 对象。推荐命名：

- `start_record` / `stop_record` — 录音控制
- `display_text` — 显示文本
- `play_audio` — 播放音频
- `transcribe` — 语音转文字
- `sync` — 同步数据
- `set_led` — 设置 LED

## 配置示例

在 `~/.gleam/settings.yaml` 中配置设备：

```yaml
hardware:
  xiaohong:
    base_url: "http://192.168.1.100:8080"
    api_key: "optional"
  qianwen_a2:
    port: "COM3"
    serial_id: "optional"
```

## 开发调试

```bash
# 列出所有注册设备
gleam goal "列出所有 AI 硬件设备" --mock-llm

# 连接小鸿
gleam goal "连接到小鸿设备" --mock-llm

# 发送命令
gleam goal "让小鸿显示'你好'" --mock-llm
```

## 安全

- `hardware.list` / `hardware.receive` / `hardware.info` — 只读，自动放行
- `hardware.connect` / `hardware.disconnect` / `hardware.send` — 需用户批准

所有硬件操作都经过安全门控，高风险命令（如固件更新）建议设置为 `PermissionFullAccess`。
