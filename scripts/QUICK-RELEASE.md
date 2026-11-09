# Gleam 快速发布指南 🚀

## 5 分钟完成一次完整发布

### 前置条件
- [x] Git tag 已创建（例如 `v0.0.34`）
- [x] 代码已提交并推送到远程仓库
- [x] Go 1.22+ 已安装并配置 PATH
- [ ] （可选）GitHub Token 已配置（用于自动上传）

---

## 一键发布命令

```bash
cd /d/PersonalProject/Gleam
bash scripts/release-all.sh --version v0.0.34
```

等待 5-10 分钟，脚本会自动完成：
1. ✅ 环境检测 → 验证 Go 和项目结构
2. ✅ 版本确认 → 从 Git tag 获取版本号
3. ✅ 构建所有平台 → Windows/Linux/macOS (AMD64/ARM64)
4. ✅ 生成校验和 → SHA256 验证文件完整性
5. ✅ 创建归档 → 压缩发布包
6. ✅ 上传 GitHub → 自动创建 Release（需要 Token）

---

## 常见场景

### 场景 1: 只本地测试（不上传）
```bash
bash scripts/release-all.sh --version v0.0.35 --no-upload
```

### 场景 2: 模拟运行（查看将要做什么）
```bash
bash scripts/release-all.sh --version v0.0.35 --dry-run
```

### 场景 3: 跳过预检查（快速发布）
```bash
bash scripts/release-all.sh --version v0.0.35 --skip-verify
```

### 场景 4: 指定版本格式
```bash
VERSION=v0.0.35-beta.1 bash scripts/release-all.sh
```

---

## 发布后验证清单

### ✅ 检查构建产物
```bash
ls -lh dist/windows/amd64/*.exe    # Windows 版本 (~16MB)
ls -lh dist/linux/amd64/*.tar.gz   # Linux AMD64
ls -lh dist/darwin/arm64/*.tar.gz  # macOS ARM64
```

### ✅ 验证校验和
```bash
cd dist/windows/amd64
sha256sum -c checksums.txt

cd ../../linux/amd64  
sha256sum -c checksums.txt
```

### ✅ 检查 GitHub Release
```
访问：https://github.com/GoodTime-AI/Gleam/releases/tag/v0.0.34
确认：所有资产已上传 ✓
确认：Release Notes 已生成 ✓
```

---

## 故障排除速查表

| 问题 | 解决方案 |
|------|---------|
| `bash: command not found` | 使用 Git Bash 或 WSL，不要用 CMD |
| `Go is not installed` | 设置 `export PATH=$PATH:/c/Program\ Files/Go/bin` |
| `GitHub token error` | 生成新 Token: https://github.com/settings/tokens |
| `Build failed for darwin` | 非 Mac 系统无法构建 macOS，可忽略 |
| `Git tag not found` | 先执行 `git tag v0.0.34 && git push origin v0.0.34` |

---

## 手动上传 GitHub（备选方案）

如果自动上传失败，可以手动操作：

1. **访问**: https://github.com/GoodTime-AI/Gleam/releases/new
2. **Tag version**: `v0.0.34`
3. **Target**: `master` branch
4. **Title**: `Gleam v0.0.34`
5. **描述**: 复制 `dist/CHECKSUMS-v0.0.34.txt` 内容
6. **附件**: 拖拽 `dist/` 下的所有 `.exe` 和 `.tar.gz` 文件
7. **勾选**: "Set as latest release"
8. **点击**: Publish release

---

## 时间估算

| 阶段 | Windows | Linux/macOS |
|------|---------|-------------|
| 环境检测 | ~5s | ~3s |
| 单个平台构建 | ~30-60s | ~20-40s |
| 全部平台构建 | ~5-8 分钟 | ~2-3 分钟 |
| 校验和生成 | <1s | <1s |
| GitHub 上传 | ~1 分钟 | ~1 分钟 |
| **总计** | **~7-10 分钟** | **~3-5 分钟** |

⚡ **提示**: 首次构建会缓存依赖，后续更快

---

## 常见问题 FAQ

**Q: 能否同时构建多个版本？**  
A: 建议逐个版本发布，避免冲突。每个版本单独 Tag。

**Q: 需要在特定操作系统上构建吗？**  
A: 不需要！交叉编译支持完整。Windows/Mac/Linux 都能构建所有平台。

**Q: ARM64 构建失败怎么办？**  
A: 如果不是在 ARM 机器上，ARM64 可能失败。这是正常的，可以注释掉目标。

**Q: 如何回滚到旧版本？**  
A: 直接部署旧版本的 Release assets 即可，无需特殊操作。

**Q: 构建产物需要签名吗？**  
A: Windows 建议 CodeSign，但 Go 自包含 exe 通常能正常运行（可能有 SmartScreen 警告）。

---

## 下一步

发布完成后：
1. 更新 CHANGELOG.md
2. 通知用户（Discord/Twitter/邮件列表）
3. 监控 GitHub Issues 中的反馈
4. 记录本次发布的特殊注意事项

🎉 **恭喜！你已经成功发布了一个新版本！**
