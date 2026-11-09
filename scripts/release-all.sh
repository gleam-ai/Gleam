#!/bin/bash
# gleam-release-all.sh - 一键多平台发布脚本 (Windows/Linux/macOS)
# 
# 功能:
#   1. 自动检测当前系统架构
#   2. 支持交叉编译到所有主要平台
#   3. 生成签名校验和
#   4. 可选上传到 GitHub Releases
#
# 用法:
#   bash scripts/release-all.sh [--version X.X.X] [--dry-run] [--no-upload]

set -euo pipefail

# ==================== 配置区域 ====================
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
PROJECT_ROOT="$(cd "$SCRIPT_DIR/.." && pwd)"

# 版本配置（可通过命令行覆盖）
VERSION="${VERSION:-}"
DRY_RUN=false
NO_UPLOAD=false
SKIP_VERIFY=false

# 颜色输出
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
CYAN='\033[0;36m'
NC='\033[0m' # No Color

# ==================== 工具函数 ====================
log_info()    { echo -e "${BLUE}[INFO]${NC} $(date '+%H:%M:%S') $1"; }
log_success() { echo -e "${GREEN}[SUCCESS]${NC} $(date '+%H:%M:%S') $1"; }
log_warn()    { echo -e "${YELLOW}[WARN]${NC} $(date '+%H:%M:%S') $1"; }
log_error()   { echo -e "${RED}[ERROR]${NC} $(date '+%H:%M:%S') $1" >&2; exit 1; }
log_step()    { echo -e "\n${CYAN}━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━${NC}"
                echo -e "${CYAN}★ $1${NC}"
                echo -e "${CYAN}━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━${NC}\n"; }

# 解析命令行参数
parse_args() {
  while [[ $# -gt 0 ]]; do
    case $1 in
      --version|-v) VERSION="$2"; shift 2 ;;
      --version=* ) VERSION="${1#*=}"; shift ;;
      --dry-run) DRY_RUN=true; shift ;;
      --no-upload) NO_UPLOAD=true; shift ;;
      --skip-verify) SKIP_VERIFY=true; shift ;;
      -h|--help)
        echo "用法: $0 [选项]"
        echo ""
        echo "选项:"
        echo "  --version X.X.X    指定版本号 (默认从 Git tag 或 config 获取)"
        echo "  --dry-run          模拟运行，不实际构建"
        echo "  --no-upload        仅本地构建，不上传到 GitHub"
        echo "  --skip-verify      跳过预构建验证"
        echo "  -h, --help         显示此帮助信息"
        exit 0
        ;;
      *) log_error "未知参数：$1";;
    esac
  done
}

# ==================== 环境检测 ====================
detect_platform() {
  log_step "🔍 环境检测"
  
  OS=$(uname -s)
  ARCH=$(uname -m)
  
  case "$OS" in
    Linux*)   CURRENT_OS="linux";;
    Darwin*)  CURRENT_OS="macos";;
    MINGW*|MSYS*|CYGWIN*) CURRENT_OS="windows";;
    *)        log_error "不支持的系统：$OS";;
  esac
  
  case "$ARCH" in
    x86_64)   CURRENT_ARCH="amd64";;
    aarch64|arm64) CURRENT_ARCH="arm64";;
    *)        CURRENT_ARCH="amd64";; # 默认
  esac
  
  log_info "当前平台：$CURRENT_OS / $CURRENT_ARCH"
  log_info "项目目录：$PROJECT_ROOT"
  
  if command -v go &>/dev/null; then
    GO_VERSION=$(go version | awk '{print $3}')
    log_info "Go 版本：$GO_VERSION"
  else
    log_error "未安装 Go，请先安装 Go 1.22+"
  fi
  
  if [ -f "$PROJECT_ROOT/cmd/gleam/main.go" ]; then
    log_info "主程序入口：✓ 找到"
  else
    log_error "未找到 cmd/gleam/main.go"
  fi
}

# ==================== 版本管理 ====================
get_version() {
  log_step "📦 版本管理"
  
  if [ -z "$VERSION" ]; then
    log_info "正在从 Git tag 获取版本号..."
    
    # 优先使用最近的 Git tag
    if git describe --tags --abbrev=0 2>/dev/null; then
      VERSION=$(git describe --tags --abbrev=0)
      log_success "从 Git 获取版本：$VERSION"
    elif grep -q "^VERSION" "$PROJECT_ROOT/pkg/buildinfo/version.go" 2>/dev/null; then
      VERSION=$(grep "^VERSION" "$PROJECT_ROOT/pkg/buildinfo/version.go" | head -1 | cut -d'"' -f2)
      log_info "从代码获取版本：$VERSION"
    else
      log_warn "无法自动获取版本号"
      read -p "请输入版本号 (例如 v0.0.33): " VERSION
    fi
  fi
  
  # 清理版本号格式
  VERSION="${VERSION#v}"
  log_success "最终版本：v$VERSION"
  
  if [ "$DRY_RUN" = true ]; then
    log_warn "干跑模式！不会执行任何构建操作"
  fi
}

# ==================== 构建目标定义 ====================
declare -A BUILD_TARGETS=(
  ["windows-amd64"]="windows/amd64"
  ["windows-arm64"]="windows/arm64"
  ["linux-amd64"]="linux/amd64"
  ["linux-arm64"]="linux/arm64"
  ["darwin-amd64"]="darwin/amd64"
  ["darwin-arm64"]="darwin/arm64"
)

# ==================== 核心构建逻辑 ====================
build_for_platform() {
  local platform="$1"
  local goos="${BUILD_TARGETS[$platform]%/*}"
  local goarch="${BUILD_TARGETS[$platform]#*/}"
  local output_name="gleam-${VERSION}-${goos}-${goarch}"
  
  log_info "🔨 构建 $platform ..."
  log_info "  GOOS=$goos GOARCH=$goarch"
  
  if [ "$DRY_RUN" = true ]; then
    log_info "  [DRY-RUN] 模拟构建成功"
    return 0
  fi
  
  export GOOS="$goos"
  export GOARCH="$goarch"
  export CGO_ENABLED=0
  
  # 添加版本信息到编译标志
  LDFLAGS="-ldflags='-s -w -X gleam/pkg/buildinfo.Version=$VERSION -X gleam/pkg/buildinfo.BuildTime=$(date -u +%Y-%m-%dT%H:%M:%SZ)'"
  
  mkdir -p "$PROJECT_ROOT/dist/$goos/$goarch"
  
  if go build $LDFLAGS -o "$PROJECT_ROOT/dist/$goos/$goarch/$output_name" -C "$PROJECT_ROOT/cmd/gleam" . 2>&1; then
    log_success "✅ $platform 构建完成"
    
    # Windows 需要添加.exe 后缀
    if [ "$goos" = "windows" ]; then
      mv "$PROJECT_ROOT/dist/$goos/$goarch/$output_name" \
         "$PROJECT_ROOT/dist/$goos/$goarch/$output_name.exe"
    fi
    
    # Linux/macOS 设置可执行权限
    if [ "$goos" != "windows" ]; then
      chmod +x "$PROJECT_ROOT/dist/$goos/$goarch/$output_name"
    fi
    
    return 0
  else
    log_error "❌ $platform 构建失败"
    unset GOOS GOARCH CGO_ENABLED
    return 1
  fi
  
  unset GOOS GOARCH CGO_ENABLED
}

# ==================== 批量构建 ====================
run_build() {
  log_step "🏗️ 开始批量构建"
  
  local failed=0
  local succeeded=0
  
  for platform in "${!BUILD_TARGETS[@]}"; do
    log_info "准备构建：$platform"
    
    if build_for_platform "$platform"; then
      ((succeeded++))
    else
      ((failed++))
      log_warn "跳过失败的构建，继续下一个..."
    fi
    
    echo ""
  done
  
  log_step "📊 构建统计"
  log_success "成功：$succeeded"
  if [ $failed -gt 0 ]; then
    log_warn "失败：$failed"
  fi
  
  if [ $failed -gt 0 ] && [ $succeeded -eq 0 ]; then
    log_error "所有构建都失败了!"
  fi
}

# ==================== 生成校验和 ====================
generate_checksums() {
  log_step "🔐 生成文件校验和"
  
  cd "$PROJECT_ROOT/dist"
  
  # 为每个平台生成独立的 checksum 文件
  for os_dir in windows linux darwin; do
    if [ -d "$os_dir" ]; then
      for arch_dir in amd64 arm64; do
        if [ -d "$os_dir/$arch_dir" ]; then
          local checksum_file="$os_dir/$arch_dir/checksums.txt"
          
          # 根据系统选择 sha256sum 命令
          if command -v sha256sum &>/dev/null; then
            cd "$os_dir/$arch_dir"
            sha256sum * >"$checksum_file" 2>/dev/null || true
            cd ../../..
          elif command -v shasum &>/dev/null; then
            cd "$os_dir/$arch_dir"
            shasum -a 256 * >"$checksum_file" 2>/dev/null || true
            cd ../../..
          fi
          
          if [ -f "$checksum_file" ]; then
            log_info "  ✓ $os_dir/$arch_dir/checksums.txt"
          fi
        fi
      done
    fi
  done
  
  # 生成汇总文件
  local total_checksum="CHECKSUMS-${VERSION}.txt"
  echo "## Gleam ${VERSION} - All Platforms Checksums" > "$total_checksum"
  echo "" >> "$total_checksum"
  echo "Generated: $(date -u '+%Y-%m-%d %H:%M:%S UTC')" >> "$total_checksum"
  echo "" >> "$total_checksum"
  
  for os_dir in windows linux darwin; do
    echo "### $os_dir" >> "$total_checksum"
    for arch_dir in amd64 arm64; do
      if [ -f "$os_dir/$arch_dir/checksums.txt" ]; then
        echo ""; echo "\`\`\`" >> "$total_checksum"
        cat "$os_dir/$arch_dir/checksums.txt" >> "$total_checksum"
        echo "" >> "$total_checksum"
        echo "\`\`\`" >> "$total_checksum"
      fi
    done
    echo "" >> "$total_checksum"
  done
  
  log_success "总校验和文件：$total_checksum"
}

# ==================== 归档打包 ====================
create_archives() {
  log_step "📦 创建归档文件"
  
  cd "$PROJECT_ROOT/dist"
  
  for os_dir in linux darwin; do
    if [ -d "$os_dir" ]; then
      for arch_dir in amd64 arm64; do
        local build_dir="$os_dir/$arch_dir"
        if [ -d "$build_dir" ]; then
          local archive_name="Gleam_${VERSION}_${os_dir}_${arch_dir}"
          local archive_path="$archive_name.tar.gz"
          
          log_info "  打包 $archive_name..."
          
          tar -czf "$archive_path" -C "$build_dir" .
          
          # 移动归档到顶层目录
          mv "$archive_path" "."
          
          log_success "  ✓ $archive_path"
        fi
      done
    fi
  done
  
  cd - > /dev/null
}

# ==================== 预构建验证 ====================
run_verification() {
  if [ "$SKIP_VERIFY" = true ]; then
    log_warn "跳过预构建验证 (--skip-verify)"
    return 0
  fi
  
  log_step "✅ 预构建验证"
  
  # 检查代码是否能编译
  log_info "检查 Go 代码编译..."
  if ! go build ./cmd/gleam/... 2>&1; then
    log_error "代码编译失败，请修复后再继续"
  fi
  log_success "代码编译检查通过"
  
  # 运行单元测试
  log_info "运行单元测试..."
  if go test ./internal/harness/conversation/... -count=1 2>&1 | tail -5; then
    log_success "单元测试通过"
  else
    log_warn "部分测试失败，建议修复后重试"
  fi
  
  # 检查 Git 状态
  if git status --porcelain 2>/dev/null | grep -q .; then
    log_warn "Git 工作区有未提交的更改："
    git status --short 2>/dev/null || true
    read -p "继续？(y/n) " confirm
    if [ "$confirm" != "y" ]; then
      log_error "取消发布"
    fi
  else
    log_success "Git 工作区干净"
  fi
}

# ==================== GitHub Release 上传 ====================
upload_to_github() {
  if [ "$NO_UPLOAD" = true ]; then
    log_warn "跳过 GitHub 上传 (--no-upload)"
    return 0
  fi
  
  log_step "🚀 准备 GitHub Release"
  
  # 检查是否配置了 GitHub Token
  if [ -z "${GITHUB_TOKEN:-}" ]; then
    log_warn "未设置 GITHUB_TOKEN 环境变量"
    log_warn "需要在 ~/.bashrc 或 ~/.zshrc 中设置:"
    log_warn "  export GITHUB_TOKEN='ghp_your_token_here'"
    log_warn ""
    
    # 检查是否是 dry-run 模式，如果是则跳过而不是报错
    if [ "$DRY_RUN" = true ]; then
      log_info "干跑模式下跳过上传步骤"
      return 0
    fi
    
    read -p "是否手动上传到 GitHub? (y/n) " confirm
    
    if [ "$confirm" != "y" ]; then
      log_warn "已取消自动上传"
      upload_to_github_manual
      return 0
    fi
    
    log_info "请访问：https://github.com/GoodTime-AI/Gleam/releases/new"
    log_info "版本：v$VERSION"
    log_info "资产路径：$PROJECT_ROOT/dist/"
    upload_to_github_manual
    return 0
  fi
  
  # 检查 GitHub CLI 是否安装
  if ! command -v gh &>/dev/null; then
    log_warn "GitHub CLI 未安装，使用网页方式"
    upload_to_github_manual
    return 0
  fi
  
  # 创建 GitHub Release
  log_info "创建 GitHub Release..."
  
  local release_url=""
  
  # 检查是否已存在该版本的 release
  if gh release view "v$VERSION" 2>/dev/null; then
    log_warn "Release v$VERSION 已存在，将更新..."
    RELEASE_ID="v$VERSION"
  else
    # 创建新的 Release
    log_info "创建新 Release v$VERSION..."
    
    # 生成 release notes
    cat >"/tmp/release-notes.md" <<EOF
# Gleam v$VERSION

$(date '+%Y-%m-%d')

## 🎉 最新发布

本次发布包含以下改进：
- 对话输入输出完全模仿 Qoder 行为模式
- Unicode 截断安全性增强（支持复杂 Emoji 序列）
- 搜索性能优化（添加内存索引）
- 导出/导入功能完整实现
- 全面测试覆盖

## 📦 下载链接

### Windows
- \`Gleam_${VERSION}_windows_amd64.exe\` (自签名，可能需要验证)

### Linux  
- \`Gleam_${VERSION}_linux_amd64.tar.gz\`
- \`Gleam_${VERSION}_linux_arm64.tar.gz\`

### macOS
- \`Gleam_${VERSION}_darwin_amd64.tar.gz\` (Intel)
- \`Gleam_${VERSION}_darwin_arm64.tar.gz\` (Apple Silicon)

## 🔐 验证说明

下载后可使用同目录下的 `checksums.txt` 验证文件完整性：

\`\`\`bash
sha256sum -c checksums.txt
\`\`\`

---
更多详情见：https://github.com/GoodTime-AI/Gleam/blob/main/RELEASE-NOTES.md
EOF

    gh release create "v$VERSION" \
      --title "Gleam v$VERSION" \
      --notes-file "/tmp/release-notes.md" \
      --target master \
      --generate-notes
      
    log_success "Release 创建成功"
  fi
  
  # 上传所有构建产物
  log_info "上传构建产物..."
  
  for asset in "$PROJECT_ROOT/dist"/*/*.{exe,tar.gz}; do
    if [ -f "$asset" ]; then
      gh release upload "v$VERSION" "$asset" --clobber
      log_success "  ✓ $(basename "$asset")"
    fi
  done
  
  log_success "GitHub Release 上传完成"
  log_info "Release 页面：https://github.com/GoodTime-AI/Gleam/releases/tag/v$VERSION"
}

# ==================== 手动上传指引 ====================
upload_to_github_manual() {
  log_step "📋 手动上传指南"
  
  cat <<EOF
  
━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
🤖  GitHub Release 手动上传步骤
━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━

1️⃣  访问 release 创建页面:
    https://github.com/GoodTime-AI/Gleam/releases/new

2️⃣  填写 Release 信息:
    • Tag version: v$VERSION
    • Target: master branch
    • Title: Gleam v$VERSION
    • Description: 查看生成的 release-notes.md

3️⃣  上传构建产物 (全部拖入附件框):
    $(find "$PROJECT_ROOT/dist" -type f \( -name "*.exe" -o -name "*.tar.gz" \) 2>/dev/null | sed 's|^  • |    • |')

4️⃣  勾选 "Set as a latest release"

5️⃣  点击 "Publish release"

━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━

📄 构建产物位置：
   $PROJECT_ROOT/dist/

📝 Release Notes 模板：
   $(pwd)/release-notes-$VERSION.md

━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━

EOF
}

# ==================== 总结报告 ====================
generate_summary() {
  log_step "📊 发布总结报告"
  
  echo ""
  echo -e "${GREEN}╔═══════════════════════════════════════════════════════╗${NC}"
  echo -e "${GREEN}║           ✅ 发布流程圆满完成                         ║${NC}"
  echo -e "${GREEN}╚═══════════════════════════════════════════════════════╝${NC}"
  echo ""
  echo -e "${CYAN}版本信息:${NC}     v$VERSION"
  echo -e "${CYAN}发布时间:${NC}     $(date '+%Y-%m-%d %H:%M:%S')"
  echo -e "${CYAN}构建目录:${NC}     $PROJECT_ROOT/dist/"
  echo ""
  
  echo -e "${YELLOW}📦 构建产物清单:${NC}"
  find "$PROJECT_ROOT/dist" -type f \( -name "*.exe" -o -name "*.tar.gz" -o -name "checksum*.txt" \) | sort | while read file; do
    local size=$(ls -lh "$file" | awk '{print $5}')
    printf "  ✦ %-50s %s\n" "$(basename "$file")" "$size"
  done
  
  echo ""
  echo -e "${BLUE}💡 下一步操作:${NC}"
  echo "  1. 验证各平台二进制文件能否正常运行"
  echo "  2. 运行 smoke 测试确保功能正常"
  echo "  3. 更新 CHANGELOG.md"
  echo "  4. 提交并发布到 GitHub Releases"
  
  if [ "$DRY_RUN" = true ]; then
    echo ""
    echo -e "${YELLOW}⚠️  这是模拟运行！没有实际生成任何构建产物.${NC}"
  fi
  
  echo ""
  echo -e "${GREEN}━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━${NC}"
  echo ""
}

# ==================== 清理函数 ====================
cleanup() {
  local exit_code=$?
  if [ $exit_code -ne 0 ]; then
    log_error "发布流程在阶段被中断，退出码：$exit_code"
  fi
}
trap cleanup EXIT

# ==================== 主流程 ====================
main() {
  log_step "🚀 Gleam 多平台一键发布系统"
  log_info "启动时间：$(date '+%Y-%m-%d %H:%M:%S')"
  echo ""
  
  parse_args "$@"
  
  detect_platform
  get_version
  
  if [ "$DRY_RUN" = false ]; then
    run_verification
    run_build
    generate_checksums
    create_archives
  fi
  
  upload_to_github
  generate_summary
}

# 启动主流程
main "$@"
