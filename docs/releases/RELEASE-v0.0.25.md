# Gleam v0.0.25 更新日志 - 电脑自动化集成

**发布日期**: 2026-11-02  
**版本**: v0.0.25-cua-integration

## 🎯 核心功能：Computer Use Automation (CUA)

本次更新实现了**AI Agent 直接控制桌面应用和浏览器**的能力，标志着 Gleam 从「计划执行者」进化为真正的「桌面助手」。

### 新增能力

- ✅ **自然语言控制桌面**: "打开 Chrome 访问 github.com", "在微信中发送消息给测试人员"
- ✅ **URL 自动检测触发**: 任务中出现 URL 时自动调用电脑操作功能
- ✅ **Windows UI Automation**: 结构化控件识别替代坐标点击，支持中文应用精确操作
- ✅ **IME 中文输入支持**: 微信、QQ、钉钉等应用的输入法处理
- ✅ **安全黑名单机制**: 默认阻止 localhost/127.0.0.1，命令注入防护
- ✅ **零后台进程设计**: 事件触发模式，内存占用仅 ~45KB

### 技术实现

- **适配器层**: `internal/agent/tools/cua/adapter.go` (200+ 行)
- **路由器**: `internal/agent/taskrouter/*` (智能 URL 检测和路由)
- **安全沙箱**: `internal/harness/safety/cua_sandbox.go` (白名单 + 审计追踪)
- **多端集成**: Desktop Electron + VS Code 扩展双重支持

## 📦 发布产物

| 平台 | 格式 | 大小 | 链接 |
|------|------|------|------|
| Windows | NSIS Installer | ~127 MB | [下载](https://github.com/gleam-ai/Gleam/releases/latest) |
| macOS Intel | Binary | ~15.8 MB | [下载](https://github.com/gleam-ai/Gleam/releases/latest) |
| macOS Apple Silicon | Binary | ~15.0 MB | [下载](https://github.com/gleam-ai/Gleam/releases/latest) |
| Linux x64 | Binary | ~15.8 MB | [下载](https://github.com/gleam-ai/Gleam/releases/latest) |
| Linux ARM64 | Binary | ~10.9 MB | [下载](https://github.com/gleam-ai/Gleam/releases/latest) |

## 🔧 快速开始

```bash
# Windows 一键安装
# 下载 gleam-0.0.25-setup.exe 后双击即可

# CLI 使用 (所有平台)
./gleam app                                    # 启动桌面窗口
./gleam goal "打开 Chrome 并访问 github.com"    # 电脑自动化
./gleam goal "比较 https://github.com/gleam-ai/Gleam 和竞品"  # URL 自动检测
```

## 🛡️ 安全特性

- **白名单应用**: 微信、QQ、钉钉、企业微信、Microsoft Teams 等 8+ 中文应用
- **命令拦截**: 阻止 exec/shell 等危险操作
- **审计追踪**: 完整记录所有操作到磁盘日志
- **事件触发**: 无常驻后台进程，完全按需运行

## 📚 文档

- [`COMPUTER_USE_INTEGRATION.md`](../reports/COMPUTER_USE_INTEGRATION.md) - 完整集成报告
- [`ONE-CLICK-RELEASE.md`](../reports/ONE-CLICK-RELEASE.md) - 一键发布指南
- [`config/taskrouter.json.example`](../../configs/taskrouter.json.example) - 配置文件模板

## 🐛 已知问题

- macOS 未签名二进制首次运行需 `chmod +x` 并在系统设置中允许
- UI Automation 在某些虚拟化和远程桌面环境可能不支持

## 👥 贡献者

感谢以下贡献者参与本次更新：
- CUA 适配器实现
- 中文应用白名单扩展
- 安全沙箱系统设计

---

**标签**: `v0.0.25` `cua` `computer-use` `automation` `desktop-control` `chinese-apps`
