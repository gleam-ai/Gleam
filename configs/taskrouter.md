# TaskRouter Configuration Guide

TaskRouter 负责将用户输入的 URL 自动路由到 CUA（Computer Use Automation）执行。

## ⚙️ 配置选项

### enabled (boolean) - 默认：true
是否启用 URL → CUA 路由功能。设为 `false` 可完全禁用此特性。

```json
{
  "enabled": false  // 禁用后所有 URL 都会被当作普通文本处理
}
```

### mode (string) - 默认："event-triggered"
选择 URL 检测策略：

| Mode | 说明 | 内存占用 | CPU 占用 | 延迟 |
|------|------|---------|---------|------|
| **event-triggered** | 仅在任务提交时检查一次 | ~2KB | <0.1% | 可接受 |
| **polling** | 每隔 N 秒扫描历史任务 | ~5MB | 0.5-1% | 5s 内 |
| **background** | 专职守护进程实时监控 | ~10MB | 持续 1% | <1s |

推荐值：**event-triggered** (零后台开销)

```json
{
  "mode": "event-triggered"
}
```

### poll_interval_secs (int) - 默认：5
仅当 `mode == "polling"` 时生效，定义扫描间隔秒数。最小值为 5 秒。

```json
{
  "poll_interval_secs": 10  // 每 10 秒检查一次
}
```

### blocked_hosts (array) - 默认：["127.0.0.1", "localhost"]
禁止访问的域名列表。匹配后立即拒绝并记录日志。

```json
{
  "blocked_hosts": [
    "127.0.0.1",
    "localhost",
    "mycompany.intranet",
    "internal-server.local"
  ]
}
```

可以通过环境变量动态覆盖：
```bash
export GLEAM_CUA_BLOCKED_DOMAINS=127.0.0.1,localhost,evil-site.com
```

### allowed_hosts (array) - 默认：[]
白名单模式。如果设置了非空数组，则**只允许**列表中的域名。

```json
{
  "allowed_hosts": [
    "google.com",
    "github.com",
    "stackoverflow.com"
  ]
}
```

> ⚠️ 注意：blocked_hosts 优先于 allowed_hosts

### force_uiautomation (boolean) - 默认：false
强制在所有平台使用 UI Automation 模式，即使不在 Windows 上。

```json
{
  "force_uiautomation": true  // Linux/Mac 上也用 UIA
}
```

或者通过环境变量控制：
```bash
export GLEAM_FORCE_UIAUTOMATION=true
```

### auto_confirm_urls (boolean) - 默认：false
是否自动执行 CUA 而不等待用户确认。谨慎开启！

```json
{
  "auto_confirm_urls": false  // 推荐：先问用户
}
```

开启后的行为：
- ❌ `auto_confirm_urls: false` → 弹出提示框询问"要访问这个网站吗？"
- ✅ `auto_confirm_urls: true` → 立即打开浏览器并执行操作

### log_path (string) - 默认：/tmp/gleam_cua_audit.log
审计日志文件路径。记录所有 URL 检测和执行情况。

```json
{
  "log_path": "/var/log/gleam/cua_audit.log"
}
```

---

## 🎯 典型配置场景

### 场景 1: 生产环境（最安全）
```json
{
  "enabled": true,
  "mode": "event-triggered",
  "blocked_hosts": ["127.0.0.1", "localhost"],
  "allowed_hosts": [],
  "auto_confirm_urls": false
}
```

### 场景 2: 开发调试（高频率测试）
```json
{
  "enabled": true,
  "mode": "polling",
  "poll_interval_secs": 5,
  "blocked_hosts": ["evil-site.com"],
  "auto_confirm_urls": true
}
```

### 场景 3: 跨平台统一（Mac/Linux + Windows）
```json
{
  "enabled": true,
  "mode": "event-triggered",
  "force_uiautomation": true,
  "blocked_hosts": ["127.0.0.1"]
}
```

---

## 🔧 环境变量优先级

以下环境变量会覆盖 JSON 配置：

| Env Var | 覆盖选项 |
|---------|----------|
| `GLEAM_CUA_ENABLED` | enabled |
| `GLEAM_CUA_MODE` | mode |
| `GLEAM_CUA_POLL_INTERVAL_SECS` | poll_interval_secs |
| `GLEAM_CUA_BLOCKED_DOMAINS` | blocked_hosts |
| `GLEAM_CUA_ALLOWED_DOMAINS` | allowed_hosts |
| `GLEAM_FORCE_UIAUTOMATION` | force_uiautomation |
| `GLEAM_CUA_AUTO_CONFIRM_URLS` | auto_confirm_urls |

示例（全部通过 env 覆盖）：
```bash
export GLEAM_CUA_ENABLED=true
export GLEAM_CUA_MODE=event-triggered  
export GLEAM_CUA_BLOCKED_DOMAINS=localhost,intranet
export GLEAM_FORCE_UIAUTOMATION=false
```

---

## 📊 性能指标

### Event Triggered 模式（默认）
- 初始内存：~2KB
- 单任务开销：<1ms CPU
- Goroutines: 0（无后台）
- 推荐度：⭐⭐⭐⭐⭐

### Polling 模式
- 初始内存：~5MB
- 周期性开销：~0.5% CPU
- Goroutines: 1（定时器循环）
- 推荐度：⭐⭐⭐

### Background 模式
- 初始内存：~10MB  
- 持续性开销：~1% CPU
- Goroutines: 2+（监控 + 队列处理）
- 推荐度：⭐⭐（除非有强实时需求）

---

## 🛡️ 安全建议

1. **始终启用 `blocked_hosts`**: 防止访问本地服务器或恶意站点
2. **先用 event-triggered**: 除非有特殊需求，不要用后台模式
3. **关闭 auto_confirm**: 避免 AI 自动执行危险操作
4. **定期查看审计日志**: 监控 URL 使用情况

---

## 📝 完整配置示例

```json
{
  "enabled": true,
  "mode": "event-triggered",
  "poll_interval_secs": 5,
  "blocked_hosts": [
    "127.0.0.1",
    "localhost",
    "intranet",
    "*.local"
  ],
  "allowed_hosts": [],
  "force_uiautomation": false,
  "auto_confirm_urls": false,
  "log_path": "/var/log/gleam/cua_audit.log"
}
```

复制此文件到项目目录作为标准配置模板！
