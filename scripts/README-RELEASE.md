# Gleam Release Scripts - 一键多平台发布指南

## 📚 概览

本目录包含完整的一键多平台发布脚本，支持：

### ✅ 功能特性
- **自动环境检测** - 自动识别当前操作系统和架构
- **交叉编译** - 从任何平台构建所有目标系统（Windows, Linux, macOS）
- **智能版本管理** - 从 Git tag、代码或命令行获取版本号
- **完整性验证** - SHA256 校验和生成与验证
- **GitHub Release 集成** - 可选的自动上传到 GitHub Releases
- **错误处理与回滚** - 失败时提供清晰的错误信息和恢复建议

### 🎯 支持的构建目标

| Platform | Architecture | Output Format |
|----------|-------------|---------------|
| Windows  | AMD64       | `.exe` (self-contained) |
| Windows  | ARM64       | `.exe` (self-contained) |
| Linux    | AMD64       | `tar.gz` archive |
| Linux    | ARM64       | `tar.gz` archive |
| macOS    | AMD64       | `tar.gz` archive |
| macOS    | ARM64       | `tar.gz` archive |

---

## 🚀 快速开始

### 方法 1: Bash (Linux/macOS/WSL) ⭐ 推荐

```bash
# 基本使用：在当前目录下构建并准备发布
bash scripts/release-all.sh

# 指定版本号
bash scripts/release-all.sh --version v0.0.34

# 只构建不上传（测试用）
bash scripts/release-all.sh --no-upload

# 干跑模式（模拟运行，查看将要执行的操作）
bash scripts/release-all.sh --dry-run --version v0.0.34

# 跳过预构建验证（快速发布）
bash scripts/release-all.sh --skip-verify --version v0.0.34
```

### 方法 2: Windows PowerShell + Git Bash

```powershell
# 在 PowerShell 中（需要先安装 Git for Windows）
cd D:\PersonalProject\Gleam
bash scripts/release-all.sh --version v0.0.34
```

### 方法 3: Windows CMD（备选方案）

```cmd
REM 从 Git Bash 调用（推荐）
bash -c "cd scripts && cmd /c release.cmd"

REM 或者直接使用上面的 Bash 脚本（需要 MinGW/WSL）
bash scripts/release-all.sh
```

---

## 🔧 高级配置

### 环境变量

可以在 shell 配置文件（~/.bashrc, ~/.zshrc）中设置以下环境变量：

```bash
# GitHub API Token（用于自动上传）
export GITHUB_TOKEN='ghp_your_personal_access_token_here'

# 默认版本号（可选）
export VERSION='v0.0.34'

# Go 代理（加速下载）
export GOPROXY=https://goproxy.cn,direct
```

**获取 GitHub Token:**
1. 访问 https://github.com/settings/tokens
2. 点击 "Generate new token"
3. 勾选权限：`repo`, `write:packages`, `delete_package`
4. 复制 token 到环境变量

### 自定义构建选项

修改 `scripts/release-all.sh` 中的 `BUILD_TARGETS` 变量：

```bash
# 只构建部分平台
declare -A BUILD_TARGETS=(
  ["windows-amd64"]="windows/amd64"
  ["linux-amd64"]="linux/amd64"
  # 注释掉不再需要的平台
  # ["darwin-arm64"]="darwin/arm64"
)
```

### 添加更多构建标志

编辑脚本中的 `run_build()` 函数，添加编译优化选项：

```bash
# 原行：
LDFLAGS="-ldflags=\"-s -w -X gleam/pkg/buildinfo.Version=$VERSION ...\""

# 改为（添加更多符号表和调试信息）：
LDFLAGS="-ldflags=\"-s -w -X gleam/pkg/buildinfo.Version=$VERSION \
         -X gleam/pkg/buildinfo.Compiler=$(go version | awk '{print $3}') \
         -X 'gleam/internal/config.BuiltBy=$(whoami)'\""
```

---

## 📋 发布流程

### 完整发布流程（推荐）

```bash
# 1️⃣ 确保代码已提交并推送
git status
git push origin master

# 2️⃣ 打标签（如果没有）
git tag v0.0.34
git push origin v0.0.34

# 3️⃣ 运行发布脚本
bash scripts/release-all.sh --version v0.0.34

# 4️⃣ 等待构建完成
# 脚本会自动构建 → 归档 → 生成 checksum → 上传到 GitHub

# 5️⃣ 验证发布结果
echo 检查：https://github.com/GoodTime-AI/Gleam/releases/tag/v0.0.34
```

### 本地测试发布

```bash
# 只构建，不上传
bash scripts/release-all.sh --version v0.0.34-test --no-upload --dry-run

# 检查生成的文件
ls -lh dist/windows/amd64/
ls -lh dist/linux/amd64/
ls -lh dist/darwin/arm64/
```

---

## 🔐 安全验证

### 验证构建产物

每个平台的二进制文件都包含校验和：

```bash
cd dist/windows/amd64
sha256sum -c checksums.txt  # 验证 Windows 版本

cd ../../linux/amd64
sha256sum -c checksums.txt  # 验证 Linux 版本
```

### 验证 GitHub Release

```bash
# 安装 GitHub CLI（如果未安装）
npm install -g @cli/cli

# 下载并验证 Release 资产
gh release download v0.0.34 --dir ./downloaded-files
cd downloaded-files
sha256sum *.exe *.tar.gz > my-checksums.txt
diff my-checksums.txt ../dist/*/checksums.txt
```

---

## 🛠️ 故障排除

### 问题 1: "bash: cannot execute: No such file or directory"

**原因**: Windows 上的换行符问题

**解决方案**:
```bash
dos2unix scripts/release-all.sh
# 或者
sed -i 's/\r$//' scripts/release-all.sh
```

### 问题 2: "Go is not installed"

**原因**: PATH 环境变量未配置

**解决方案:**
```bash
# 重新加载 Shell 配置
source ~/.bashrc

# 或者明确指定 Go 路径
GO=/path/to/go bash scripts/release-all.sh
```

### 问题 3: GitHub Token 权限不足

**症状**: 上传时收到 403 Forbidden 错误

**解决方案:**
1. 检查 Token 权限是否包含 `repo` 和 `public_repo`
2. 确认 Token 未过期
3. 使用新 Token 更新环境变量

```bash
export GITHUB_TOKEN='new-token-here'
```

### 问题 4: Cross-compilation fails on ARM Mac

**症状**: macOS ARM 上无法构建 AMD64

**解决方案:**
```bash
# Apple Silicon 需要 Intel 工具链
brew install --arm-to-intel xcode-select

# 或使用 Docker 进行交叉编译
docker run --rm -t mcr.microsoft.com/golang:1.22 \
  bash -c "go env -w GOOS=darwin GOARCH=amd64" \
          "go build ..."
```

### 问题 5: "fatal: No names found, cannot describe anything."

**症状**: Git 仓库没有标签

**解决方案:**
```bash
# 创建初始标签
git tag v0.0.34
git push origin v0.0.34

# 或直接指定版本
bash scripts/release-all.sh --version v0.0.34
```

---

## 📊 构建输出结构

执行发布后，目录结构如下：

```
dist/
├── windows/
│   └── amd64/
│       ├── gleam-v0.0.34-windows-amd64.exe
│       └── checksums.txt
├── linux/
│   └── amd64/
│       ├── gleam-v0.0.34-linux-amd64
│       └── checksums.txt
│   └── arm64/
│       ├── gleam-v0.0.34-linux-arm64
│       └── checksums.txt
├── darwin/
│   ├── amd64/
│   │   ├── gleam-v0.0.34-darwin-amd64
│   │   └── checksums.txt
│   └── arm64/
│       ├── gleam-v0.0.34-darwin-arm64
│       └── checksums.txt
├── Gleam_v0.0.34_linux_amd64.tar.gz      # 预打包压缩文件
├── Gleam_v0.0.34_darwin_arm64.tar.gz     # 预打包压缩文件
├── CHECKSUMS-v0.0.34.txt                  # 汇总校验和
└── release-notes-v0.0.34.md               # Release Notes 模板
```

---

## 🌟 最佳实践

### 1. 发布前检查清单

- [ ] 所有单元测试通过 (`go test ./...`)
- [ ] E2E 测试通过 (`bash scripts/smoke.sh`)
- [ ] Git 工作区干净 (`git status`)
- [ ] CHANGELOG.md 已更新
- [ ] 文档已同步更新

### 2. 半自动化发布（推荐用于首次发布）

```bash
# Step 1: 构建并本地验证
bash scripts/release-all.sh --version v0.0.34 --no-upload --dry-run

# Step 2: 手动下载测试各平台二进制文件
# Step 3: 确认无误后再次运行（启用上传）
bash scripts/release-all.sh --version v0.0.34
```

### 3. CI/CD集成示例

在 GitHub Actions 中添加发布流程：

```yaml
name: Multi-Platform Release

on:
  push:
    tags:
      - 'v*'

jobs:
  release:
    runs-on: ubuntu-latest
    
    steps:
      - uses: actions/checkout@v3
      
      - name: Set up Go
        uses: actions/setup-go@v4
        with:
          go-version: '1.22'
          
      - name: Build all platforms
        run: |
          export GITHUB_TOKEN=${{ secrets.GITHUB_TOKEN }}
          bash scripts/release-all.sh --no-upload
          
      - name: Upload to GitHub Release
        run: |
          gh release upload ${GITHUB_REF_NAME} dist/*/** --clobber
```

---

## 📞 支持与反馈

遇到问题或有改进建议？

1. 查看现有文档：[docs/RELEASE-NOTES.md](file:///d:/PersonalProject/Gleam/docs/RELEASE-NOTES.md)
2. 搜索 Issues: https://github.com/GoodTime-AI/Gleam/issues
3. 创建 Issue: https://github.com/GoodTime-AI/Gleam/issues/new?labels=release&title=%5BRelease%5D

---

## 📝 历史记录

- **2026-11-08**: 重写为现代化脚本，添加完整 GitHub Integration
- **2026-11-05**: 最初实现基础交叉编译功能
- **2026-10-10**: 早期版本的独立平台脚本

---

## 📄 许可证

此发布脚本遵循项目主协议的相同许可证。

**Built with ❤️ by the Gleam Team**
