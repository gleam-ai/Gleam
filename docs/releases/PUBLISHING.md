# Gleam 一键多平台发布指南

## 🚀 快速启动（全自动）

### 方式 A: Linux/macOS (推荐)
```bash
# 进入项目目录
cd /d/PersonalProject/Gleam

# 一键构建所有平台并推送 GitHub Release
bash scripts/multi-platform-release.sh

# 然后自动推送到 GitHub（需要 gh 已登录）
gh release create v0.0.25-beta.1 \
  dist/windows/*.exe \
  dist/linux/*.tar.gz \
  dist/macos/*.tar.gz \
  --notes-file /tmp/gleam-release-*/release-notes.md \
  --generate-notes
```

### 方式 B: Windows
```cmd
REM 进入项目目录
cd D:\PersonalProject\Gleam

REM 运行 PowerShell 脚本
powershell -File scripts\multi-platform-release.ps1

REM 或者使用批处理
scripts\release-to-github.bat 0.0.25-beta.1
```

---

## 📦 支持的发布平台

| 平台 | 架构 | 格式 | 状态 |
|------|------|------|------|
| **Windows** | x64 | `.exe` installer | ✅ Supported |
| **Linux** | x64 | `.tar.gz` archive | ✅ Supported |
| **Linux** | ARM64 | `.tar.gz` archive | ✅ Supported |
| **macOS Intel** | x64 | `.tar.gz` archive | ✅ Supported |
| **macOS Apple Silicon** | ARM64 | `.tar.gz` archive | ✅ Supported |

---

## 🎯 完整发布流程

### Step 1: 版本准备
```bash
# 更新版本号
cd /d/PersonalProject/Gleam
sed -i 's/^version:.*/version: 0.0.25-beta.1/' dist/latest.yml

# 提交变更
git add .
git commit -m "chore: prepare release v0.0.25-beta.1"
```

### Step 2: 构建所有平台
```bash
# 执行一键构建脚本
chmod +x scripts/multi-platform-release.sh
bash scripts/multi-platform-release.sh
```

**输出位置**:
- `dist/windows/` - Windows 安装包
- `dist/linux/` - Linux tarballs  
- `dist/macos/` - macOS archives

### Step 3: 质量检查
```bash
# 验证 checksums
cat dist/CHECKSUMS.md

# 测试下载和校验
cd dist/linux
sha256sum -c CHECKSUMS.md
```

### Step 4: 推送到 GitHub Releases
```bash
# 创建 Git tag
git tag -a "v0.0.25-beta.1" -m "Release Gleam v0.0.25-beta.1"

# 推送 tag（可选）
git push origin v0.0.25-beta.1

# 上传到 GitHub Releases
gh release create v0.0.25-beta.1 \
  dist/windows/*.exe \
  dist/linux/*.tar.gz \
  dist/macos/*.tar.gz \
  --title "Gleam v0.0.25-beta.1" \
  --notes-file dist/release-notes.md \
  --prerelease
```

---

## 🔧 配置选项

### 自定义版本号和元数据
```bash
export GLEAM_RELEASE_VERSION="0.0.25-beta.1"
export GLEAM_RELEASE_NOTES="Major improvements to computer use automation"

bash scripts/multi-platform-release.sh
```

### 选择性地构建某些平台
编辑脚本中的变量：
```bash
BUILD_WINDOWS=true   # 默认 true
BUILD_LINUX=false    # 设为 false 跳过 Linux
BUILD_MACOS=true     # 默认 true
BUILD_ARM=false      # 默认 false (ARM 构建需要对应硬件)
```

### 跳过特定步骤
```bash
# 只构建不推送
SKIP_GITHUB_PUBLISH=true bash scripts/multi-platform-release.sh

# 只测试本地构建
DRY_RUN_MODE=true bash scripts/multi-platform-release.sh
```

---

## 📊 发布产物清单

每次发布会生成以下文件：

### Windows
```
dist/windows/
└── Gleam_0.0.25-beta.1_setup/
    └── Gleam.exe          # 主程序
    ├── RELEASE-NOTES.txt   # 简要说明
    └── install.ini         # 安装配置
```

### Linux
```
dist/linux/
├── Gleam_0.0.25-linux_amd64.tar.gz
└── Gleam_0.0.25-linux_arm64.tar.gz
```

### macOS
```
dist/macos/
├── Gleam_0.0.25-macos_amd64.tar.gz      # Intel
└── Gleam_0.0.25-macos_arm64.tar.gz      # Apple Silicon
```

### 统一文件
```
dist/
├── CHECKSUMS.md           # SHA256 校验和
├── ARTIFACTS.txt          # 文件列表
├── release-notes.md       # 完整发布说明
└── latest.yml             # Electron auto-updater config
```

---

## 🔒 安全加固

### 签名要求（可选）
```bash
# Windows 代码签名
signcode signtool sign /fd SHA256 /tr http://timestamp.digicert.com ^
  dist/windows/Gleam*.exe /a

# macOS notarization (Apple)
xcrun notarytool submit dist/macos/*.pkg \
  --apple-id "your@email.com" \
  --password "@keychain:NotaryApp" \
  --team-id "YOUR_TEAM_ID"
```

### 上传前扫描
```bash
# 使用 VirusTotal API 扫描 Windows exe
python3 scripts/virustotal-scan.py dist/windows/*.exe

# 检查敏感信息泄露
grep -r "api_key\|secret\|password" dist/ || echo "✅ No secrets found"
```

---

## 📝 发布模板文件

### CHANGELOG.md
```markdown
# Changelog

## [0.0.25-beta.1] - 2026-11-02

### Added
- Computer Use Automation integration with URL detection
- Windows UI Automation mode for precise screen control
- Support for WeChat/QQ/DingTalk Chinese applications
- Zero-background event-triggered routing (~2KB memory overhead)
- Configurable security policies (blocklist/whitelist)
- Audit logging for all computer use actions

### Security
- Enhanced application whitelist (8+ Chinese apps)
- Command injection prevention (`exec`, `shell` blocked)
- Default blocked hosts enforcement (localhost, 127.0.0.1)
- Full action audit trail generation

### Performance
- Memory: ~50MB base + 2KB per CUA instance
- CPU: <1% idle state in event-triggered mode
- Zero background processes when not active

### Documentation
- Complete multi-platform build scripts
- Configuration guide for task router
- Security best practices documentation
- Release verification checklist

[0.0.25-beta.1]: https://github.com/gleam-ai/Gleam/releases/tag/v0.0.25-beta.1
```

---

## 🆘 故障排除

### 构建失败：`go build failed`
**原因**: CGO_ENABLED 未正确设置  
**解决**: 
```bash
export CGO_ENABLED=0
bash scripts/multi-platform-release.sh
```

### 签名问题
**错误**: `Signer certificate chain invalid`  
**解决**:
```bash
# 获取有效证书
certutil -store My "YourSigningCertName"

# 或使用 test cert
signcode sign /v /td sha256 /fd sha256 /tr http://timestamp.acs.microsoft.com ^
  /d "Gleam Signer" /du "https://gleam.wangjn.top" ^
  gleam.exe
```

### GitHub 上传失败
**错误**: `API rate limit exceeded`  
**解决**:
```bash
# 等待配额刷新或认证
gh auth login --scopes write:repo

# 或降低上传速度
for file in dist/*/*; do
  gh release upload v0.0.25-beta.1 "$file" --clobber
  sleep 2
done
```

---

## 🎉 发布检查清单

发布前请确认所有项目完成：

- [ ] All tests pass (`bash scripts/verify-release.sh`)
- [ ] Code properly formatted (`gofmt -l` empty)
- [ ] Static analysis clean (`go vet ./...`)
- [ ] Checksums generated and valid
- [ ] Manual testing on target platforms completed
- [ ] CHANGELOG.md updated
- [ ] Version bumped in go.mod
- [ ] git tag created
- [ ] Release notes prepared
- [ ] Security scan passed
- [ ] Signing certificates current (if required)
- [ ] CI/CD pipeline passes for release branch

---

## 📞 紧急情况回滚

如果发布后发现问题，立即执行：

```bash
# 1. 撤销 GitHub 标签
gh release delete v0.0.25-beta.1 --cleanup-tag --yes

# 2. 删除远程标签分支
git push origin :refs/tags/v0.0.25-beta.1

# 3. 发布修复版本
git tag v0.0.25.1 -m "Hotfix: Recovery from beta.1 issues"
bash scripts/multi-platform-release.sh
gh release create v0.0.25.1 dist/* --generate-notes
```

---

## ✨ 自动化扩展（可选）

### GitHub Actions 集成
创建 `.github/workflows/release.yml`:

```yaml
name: Multi-Platform Release

on:
  push:
    tags:
      - 'v*'

jobs:
  build-and-release:
    runs-on: ${{ matrix.os }}
    strategy:
      matrix:
        os: [windows-latest, ubuntu-latest, macos-latest]
    
    steps:
      - uses: actions/checkout@v3
      
      - name: Setup Go
        uses: actions/setup-go@v4
        with:
          go-version: '1.22'
      
      - name: Build for ${{ matrix.os }}
        run: |
          if [ "${{ matrix.os }}" == "windows-latest" ]; then
            go build -o gleam.exe ./cmd/gleam
          elif [ "${{ matrix.os }}" == "ubuntu-latest" ]; then
            go build -o gleam-amd64 ./cmd/gleam
          else
            go build -o gleam-macos ./cmd/gleam
          fi
      
      - name: Upload artifact
        uses: actions/upload-artifact@v3
        with:
          name: gleam-${{ matrix.os }}
          path: gleam*
      
  release:
    needs: [build-and-release]
    runs-on: ubuntu-latest
    
    steps:
      - uses: actions/download-artifact@v3
      
      - name: Create GitHub Release
        env:
          GH_TOKEN: ${{ github.token }}
        run: |
          gh release create ${{ github.ref_name }} \
            */gleam* \
            --generate-notes
```

---

## 📚 相关资源

- [RELEASE-CHECKLIST.md](RELEASE-CHECKLIST.md) - 详细发布步骤
- [config/taskrouter.md](../../configs/taskrouter.md) - 用户配置指南  
- [COMPUTER_USE_INTEGRATION.md](../reports/COMPUTER_USE_INTEGRATION.md) - 功能文档
- [scripts/verify-release.sh](../../scripts/verify-release.sh) - 验证脚本

---

**最后更新**: 2026-11-02  
**维护者**: Gleam Team  
**联系方式**: support@gleam.wangjn.top
