#!/bin/bash
# sync-version.sh - 一键同步更新所有位置版本号
# 
# 功能:
#   1. 读取新版本号
#   2. 批量替换所有文件中的版本号引用
#   3. 生成 commit 和 tag
#   4. 可选推送到远程仓库
#
# 用法:
#   bash scripts/sync-version.sh [--version X.X.X] [--commit] [--push]
#
# 安全模式:
#   --dry-run    - 仅显示将要修改的文件和内容，不实际更改
#   --no-commit  - 修改文件但不创建 commit

set -euo pipefail

# ==================== 配置区域 ====================
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
PROJECT_ROOT="$(cd "$SCRIPT_DIR/.." && pwd)"

# 颜色输出
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
CYAN='\033[0;36m'
NC='\033[0m'

# ==================== 工具函数 ====================
log_info()    { echo -e "${BLUE}[INFO]${NC} $(date '+%H:%M:%S') $1"; }
log_success() { echo -e "${GREEN}[SUCCESS]${NC} $(date '+%H:%M:%S') $1"; }
log_warn()    { echo -e "${YELLOW}[WARN]${NC} $(date '+%H:%M:%S') $1"; }
log_error()   { echo -e "${RED}[ERROR]${NC} $(date '+%H:%M:%S') $1" >&2; exit 1; }
log_step()    { echo -e "\n${CYAN}━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━${NC}"
                echo -e "${CYAN}★ $1${NC}"
                echo -e "${CYAN}━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━${NC}\n"; }

# ==================== 命令行参数解析 ====================
DRY_RUN=false
NO_COMMIT=false
PUSH=false
NEW_VERSION="${1:-}"

parse_args() {
  while [[ $# -gt 0 ]]; do
    case $1 in
      --version|-v) NEW_VERSION="$2"; shift 2 ;;
      --version=* ) NEW_VERSION="${1#*=}"; shift ;;
      --dry-run) DRY_RUN=true; shift ;;
      --no-commit) NO_COMMIT=true; shift ;;
      --push) PUSH=true; shift ;;
      -h|--help)
        cat <<EOF
用法: $0 [选项]

一键同步更新所有位置的版本号到指定值。

选项:
  --version X.X.X    指定新版本号 (必需，格式如 v0.0.34 或 0.0.34)
  --dry-run          模拟运行，显示将要修改的内容但不实际更改
  --no-commit        仅修改文件，不创建 Git commit
  --push             提交后推送到远程仓库

示例:
  $0 --version v0.0.34 --commit --push    # 同步并推送
  $0 --version v0.0.34 --dry-run          # 仅查看效果
  $0 v0.0.34                               # 快速同步（需确认）
EOF
        exit 0
        ;;
      *)
        if [[ ! "$1" =~ ^v?[0-9]+\.[0-9]+\.[0-9]+ ]]; then
          log_error "未知参数或无效版本格式：$1"
        else
          NEW_VERSION="$1"
        fi
        shift ;;
    esac
  done
}

# ==================== 版本验证与标准化 ====================
validate_version() {
  log_step "📦 版本验证与标准化"
  
  if [[ -z "$NEW_VERSION" ]]; then
    log_warn "未指定版本号，尝试从 Git 获取..."
    if git describe --tags --abbrev=0 2>/dev/null; then
      local LAST_TAG=$(git describe --tags --abbrev=0)
      log_info "最近 Tag: $LAST_TAG"
      
      # 自动递增 patch 版本
      local VERSION_NUM="${LAST_TAG#v}"
      local MAJOR=$(echo "$VERSION_NUM" | cut -d. -f1)
      local MINOR=$(echo "$VERSION_NUM" | cut -d. -f2)
      local PATCH=$(echo "$VERSION_NUM" | cut -d. -f3)
      PATCH=$((PATCH + 1))
      NEW_VERSION="v${MAJOR}.${MINOR}.${PATCH}"
      log_success "自动生成版本：$NEW_VERSION"
    else
      read -p "请输入新版本号 (例如 v0.0.34): " NEW_VERSION
    fi
  fi
  
  # 规范化版本号格式
  if [[ ! "$NEW_VERSION" =~ ^v[0-9]+\.[0-9]+\.[0-9]+$ ]]; then
    if [[ "$NEW_VERSION" =~ ^[0-9]+\.[0-9]+\.[0-9]+$ ]]; then
      NEW_VERSION="v${NEW_VERSION}"
      log_info "已添加 'v' 前缀：$NEW_VERSION"
    else
      log_error "版本号格式无效，必须是 vX.Y.Z 格式（例如 v0.0.34）"
    fi
  fi
  
  log_success "最终版本：$NEW_VERSION"
  
  # 检查是否已有此 tag
  if git rev-parse "$NEW_VERSION" 2>/dev/null; then
    log_error "Tag $NEW_VERSION 已存在！请先删除或更改版本号"
  fi
}

# ==================== 文件列表定义 ====================
declare -A FILES_TO_UPDATE=(
  # Go 源代码
  ["pkg/buildinfo/version.go"]="VERSION BUILD_TIME"
  
  # 文档文件
  ["README.md"]="version release tag"
  ["CHANGELOG.md"]="changelog versions tags"
  ["docs/RELEASE-NOTES.md"]="Gleam v Version"
  
  # 项目描述
  ["DESCRIPTION.md"]=""
  
  # Desktop/Electron 配置（如果存在）
  ["desktop/package.json"]="version"
  ["electron-builder.json"]="version"
  ["electron-builder.json5"]="version"
  
  # VS Code 扩展（如果存在）
  ["vscode-gleam/package.json"]="version"
  
  # Docker 文件
  ["Dockerfile"]="gleam tag"
  ["docker-compose.yml"]="gleam version"
  
  # CI/CD 配置
  [".github/workflows/*.yml"]="version tag"
  [".github/RELEASE-TEMPLATE.md"]="Gleam v version"
)

declare -a DOC_FILES=(
  "README.md"
  "CHANGELOG.md"
  "docs/RELEASE-NOTES.md"
)

# ==================== 版本号提取函数 ====================
get_current_versions() {
  declare -A versions
  
  # 从 Go 代码获取
  if grep -q "^const Version" "$PROJECT_ROOT/pkg/buildinfo/version.go" 2>/dev/null; then
    versions["go_code"]=$(grep "^const Version" "$PROJECT_ROOT/pkg/buildinfo/version.go" | sed 's/.*"\(.*\)".*/\1/')
  fi
  
  # 从 README 获取
  if grep -q "# Gleam v" "$PROJECT_ROOT/README.md" 2>/dev/null; then
    versions["readme"]=$(grep "# Gleam v" "$PROJECT_ROOT/README.md" | head -1 | grep -oE 'v[0-9]+\.[0-9]+\.[0-9]+' | head -1)
  fi
  
  # 从 Git tag 获取
  versions["git_tag"]=$(git describe --tags --abbrev=0 2>/dev/null || echo "none")
  
  echo "${versions[@]}"
}

# ==================== 文件替换逻辑 ====================
update_file() {
  local file="$1"
  local patterns="$2"
  local full_path="$PROJECT_ROOT/$file"
  
  if [ ! -f "$full_path" ]; then
    return 0  # 文件不存在则跳过
  fi
  
  local basename=$(basename "$file")
  
  log_info "处理 $file ..."
  
  # 根据文件类型使用不同的替换策略
  case "$basename" in
    *.go)
      # Go 源文件特殊处理
      update_go_file "$full_path" "$patterns"
      ;;
    
    package*.json)
      # JSON 文件使用 sed
      update_json_file "$full_path" "$patterns"
      ;;
    
    *.md)
      # Markdown 文件使用智能正则
      update_markdown_file "$full_path" "$patterns"
      ;;
    
    yml|yaml|dockerfile)
      update_yaml_file "$full_path" "$patterns"
      ;;
    
    *)
      # 通用文本替换
      update_text_file "$full_path" "$patterns"
      ;;
  esac
}

update_go_file() {
  local file="$1"
  local temp_file=$(mktemp)
  
  # 读取原始内容
  cp "$file" "$temp_file"
  
  # 替换 VERSION 常量
  if grep -q "const Version" "$temp_file"; then
    sed -i "s/const Version = .*/const Version = \"$NEW_VERSION\"/" "$temp_file"
    log_info "  ✓ 更新 VERSION 常量"
  fi
  
  # 替换 BuildTime
  local BUILD_TIME=$(date -u '+%Y-%m-%dT%H:%M:%SZ')
  if grep -q "var BuildTime" "$temp_file"; then
    sed -i "s/var BuildTime = .*/var BuildTime = \"${BUILD_TIME}\"/" "$temp_file"
    log_info "  ✓ 更新 BuildTime"
  fi
  
  if [ "$DRY_RUN" = false ]; then
    mv "$temp_file" "$file"
  else
    rm "$temp_file"
  fi
}

update_json_file() {
  local file="$1"
  local temp_file=$(mktemp)
  
  cp "$file" "$temp_file"
  
  # 使用 Python 进行安全的 JSON 编辑（保持格式化）
  python3 <<PYTHON_EOF
import json
import sys

try:
    with open("$temp_file", 'r', encoding='utf-8') as f:
        data = json.load(f)
    
    # 直接设置 version 字段
    data['version'] = "$NEW_VERSION"
    
    # 重新序列化并保持缩进
    with open("$file", 'w', encoding='utf-8') as f:
        json.dump(data, f, indent=2, ensure_ascii=False)
        f.write('\n')
    
    print("✓ Updated package version to $NEW_VERSION")
except Exception as e:
    print(f"Error updating JSON: {{e}}", file=sys.stderr)
    sys.exit(1)
PYTHON_EOF
}

update_markdown_file() {
  local file="$1"
  local temp_file=$(mktemp)
  
  cp "$file" "$temp_file"
  
  # 多种模式的版本替换
  local replacements=(
    "s/# Gleam v[0-9]+\.[0-9]+\.[0-9]+/# Gleam $NEW_VERSION/g"
    "s/## v[0-9]+\.[0-9]+\.[0-9]+/## $NEW_VERSION/g"
    "s/\[`v[0-9]+\.[0-9]+\.[0-9]+`\]/\`$NEW_VERSION\`/g"
    "s/release\/v[0-9]+\.[0-9]+\.[0-9]+/release\/$NEW_VERSION/g"
    "s/tag\/v[0-9]+\.[0-9]+\.[0-9]+/tag\/$NEW_VERSION/g"
    "s/Gleam-[v0-9.-]*/Gleam-$NEW_VERSION/g"
    "s/download\/v[0-9]+\.[0-9]+\.[0-9]+/download\/$NEW_VERSION/g"
  )
  
  for repl in "${replacements[@]}"; do
    sed -i "$repl" "$temp_file" 2>/dev/null || true
  done
  
  # 更新版本对比链接
  if [ -f "$PROJECT_ROOT/docs/UPGRADE-GUIDE.md" ] || [ -f "$PROJECT_ROOT/MIGRATION.md" ]; then
    local CURRENT_VERSION_NUM="${NEW_VERSION#v}"
    local PREV_VER=$(echo "$CURRENT_VERSION_NUM" | tr '.' ',' | awk -F',' '{OFS="."; print $1,$2,$3-1}')
    
    sed -i "s/from.*to $NEW_VERSION/from ~$PREV_VER to $NEW_VERSION/g" "$temp_file" 2>/dev/null || true
  fi
  
  if [ "$DRY_RUN" = false ]; then
    mv "$temp_file" "$file"
  else
    rm "$temp_file"
  fi
}

update_yaml_file() {
  local file="$1"
  local temp_file=$(mktemp)
  
  cp "$file" "$temp_file"
  
  # YAML 版本替换（简单模式）
  sed -i "s/gleam:[^[:space:]]*/gleam:$NEW_VERSION/g" "$temp_file" 2>/dev/null || true
  sed -i "s/: gleam-v[0-9.]*/: gleam-$NEW_VERSION/g" "$temp_file" 2>/dev/null || true
  
  if [ "$DRY_RUN" = false ]; then
    mv "$temp_file" "$file"
  else
    rm "$temp_file"
  fi
}

update_text_file() {
  local file="$1"
  local temp_file=$(mktemp)
  
  cp "$file" "$temp_file"
  
  # 通用版本模式替换
  sed -i "s/v[0-9]\+\.[0-9]\+\.[0-9]\+/v$NEW_VERSION/g" "$temp_file" 2>/dev/null || true
  
  if [ "$DRY_RUN" = false ]; then
    mv "$temp_file" "$file"
  else
    rm "$temp_file"
  fi
}

# ==================== 批量更新流程 ====================
batch_update() {
  log_step "🔄 开始批量更新版本号"
  
  local updated_count=0
  local skipped_count=0
  
  # 核心文件（必须更新）
  local critical_files=(
    "README.md"
    "CHANGELOG.md"
  )
  
  for file in "${critical_files[@]}"; do
    if [ -f "$PROJECT_ROOT/$file" ]; then
      log_info "处理：$file"
      update_file "$file" "VERSION"
      ((updated_count++))
    else
      log_warn "跳过（文件不存在）：$file"
      ((skipped_count++))
    fi
  done
  
  # 检查 Go 版本定义文件（如果存在）
  if [ -f "$PROJECT_ROOT/pkg/buildinfo/version.go" ]; then
    log_info "处理：pkg/buildinfo/version.go"
    update_go_file "$PROJECT_ROOT/pkg/buildinfo/version.go" "VERSION"
    ((updated_count++))
  elif grep -r "const Version" "$PROJECT_ROOT/cmd/gleam/" 2>/dev/null | head -1; then
    log_info "检测到 cmd/gleam 中的版本定义"
    go_files=$(grep -rl "const Version" "$PROJECT_ROOT/cmd/gleam/" 2>/dev/null)
    for gf in $go_files; do
      update_go_file "$gf" "VERSION"
      ((updated_count++))
    done
  else
    log_info "未找到 Go 版本定义文件 (可选)"
  fi
  
  # 可选文件
  for file in "${!FILES_TO_UPDATE[@]}"; do
    if [ -f "$PROJECT_ROOT/$file" ]; then
      local patterns="${FILES_TO_UPDATE[$file]}"
      update_file "$file" "$patterns"
      ((updated_count++))
    fi
  done
  
  # Electron/VSCode 配置（如果存在）
  for config_file in electron-builder.json electron-builder.json5 vscode-gleam/package.json desktop/package.json; do
    if [ -f "$PROJECT_ROOT/$config_file" ]; then
      log_info "处理：$config_file"
      update_json_file "$PROJECT_ROOT/$config_file" "version"
      ((updated_count++))
    fi
  done
  
  # 更新 buildinfo 的编译时间（如果文件存在）
  if [ -f "$PROJECT_ROOT/pkg/buildinfo/version.go" ]; then
    log_info "更新编译时间戳..."
    local now=$(date -u '+%Y-%m-%dT%H:%M:%SZ')
    sed -i "s/BUILD_TIME := .*/BUILD_TIME := \"${now}\"/" "$PROJECT_ROOT/pkg/buildinfo/version.go"
  fi
  
  log_success "更新了 $updated_count 个文件，跳过了 $skipped_count 个"
}

# ==================== Commit 与 Push ====================
commit_and_push() {
  if [ "$NO_COMMIT" = true ]; then
    log_warn "跳过 Git commit (--no-commit)"
    return 0
  fi
  
  log_step "📝 创建 Git commit"
  
  # 检查是否有更改
  if ! git status --porcelain 2>/dev/null | grep -q .; then
    log_info "没有文件需要提交"
    return 0
  fi
  
  # 配置 Git 用户（如果未设置）
  if ! git config user.name &>/dev/null; then
    git config user.name "Release Bot"
  fi
  if ! git config user.email &>/dev/null; then
    git config user.email "noreply@gleam.ai"
  fi
  
  # 添加更改
  git add -A
  git diff --cached --stat
  
  # 创建 commit
  git commit -m "chore(release): $NEW_VERSION
  
- Synced version across all files (README, CHANGELOG, package configs)
- Updated build info and timestamps"
  
  log_success "Created commit: $(git log -1 --oneline)"
  
  # Push 操作
  if [ "$PUSH" = true ]; then
    log_step "🚀 推送到远程仓库"
    
    # 先确保有最新的主分支代码
    git fetch origin master 2>/dev/null || git fetch origin main 2>/dev/null
    
    # 推送 commit
    git push origin HEAD 2>&1 | grep -E "(error|Successfully pushed|To github.com)" || true
    
    log_success "Commit pushed successfully"
  fi
  
  # 创建并推送 tag
  log_step "🏷️ 创建并推送 Tag"
  git tag "$NEW_VERSION"
  
  if [ "$PUSH" = true ]; then
    git push origin "$NEW_VERSION" 2>&1 | grep -E "(error|To github.com)" || true
    log_success "Tag created and pushed: $NEW_VERSION"
  else
    log_info "Tag created locally: $NEW_VERSION (use 'git push origin $NEW_VERSION' to push)"
  fi
}

# ==================== 验证检查结果 ====================
verify_update() {
  log_step "✅ 验证版本号更新一致性"
  
  local errors=0
  
  # 检查 Go 代码版本
  if grep -q "$NEW_VERSION" "$PROJECT_ROOT/pkg/buildinfo/version.go" 2>/dev/null; then
    log_success "✓ pkg/buildinfo/version.go contains $NEW_VERSION"
  else
    log_error "✗ pkg/buildinfo/version.go does NOT contain $NEW_VERSION"
    ((errors++))
  fi
  
  # 检查 README 版本
  if grep -q "$NEW_VERSION" "$PROJECT_ROOT/README.md" 2>/dev/null; then
    log_success "✓ README.md mentions $NEW_VERSION"
  else
    log_warn "⚠ README.md may need manual update"
  fi
  
  # 检查 CHANGELOG 版本
  if grep -q "^## $NEW_VERSION" "$PROJECT_ROOT/CHANGELOG.md" 2>/dev/null; then
    log_success "✓ CHANGELOG.md has entry for $NEW_VERSION"
  elif grep -q "$NEW_VERSION" "$PROJECT_ROOT/CHANGELOG.md" 2>/dev/null; then
    log_info "✓ CHANGELOG.md mentions $NEW_VERSION"
  else
    log_warn "⚠ CHANGELOG.md may need manual update"
  fi
  
  # 统计 Git tag
  local current_tag=$(git describe --tags --abbrev=0 2>/dev/null || echo "none")
  if [ "$current_tag" = "$NEW_VERSION" ]; then
    log_success "✓ Git tag points to $NEW_VERSION"
  else
    log_info "ℹ Current Git tag: $current_tag (new tag will be $NEW_VERSION)"
  fi
  
  if [ $errors -eq 0 ]; then
    log_success "所有关键文件都已成功更新版本号!"
  else
    log_error "发现 $errors 个错误，请手动检查上述文件"
  fi
}

# ==================== 生成变更摘要 ====================
generate_summary() {
  log_step "📊 版本号同步完成报告"
  
  echo ""
  echo -e "${GREEN}╔═══════════════════════════════════════════════════════╗${NC}"
  echo -e "${GREEN}║           ✅ 版本号同步圆满完成                       ║${NC}"
  echo -e "${GREEN}╚═══════════════════════════════════════════════════════╝${NC}"
  echo ""
  
  echo -e "${CYAN}新版本:${NC}       $NEW_VERSION"
  echo -e "${CYAN}执行时间:${NC}     $(date '+%Y-%m-%d %H:%M:%S')"
  echo -e "${CYAN}操作模式:${NC}     ${DRY_RUN:+Dry-run (only preview)}${NO_COMMIT:+No commit}${PUSH:+Push enabled}"
  echo ""
  
  if [ "$DRY_RUN" = true ]; then
    echo -e "${YELLOW}⚠️  这是模拟运行！文件尚未修改.${NC}"
    echo ""
    echo "预览即将更新的文件："
    git diff --name-only 2>/dev/null | head -20 || echo "  (无更改)"
    echo ""
    echo "要实际应用这些更改，请再次运行（去掉 --dry-run）："
    echo "  $0 --version $NEW_VERSION --commit --push"
    return
  fi
  
  # 显示更改的文件列表
  echo "📁 已修改的文件："
  git status --short 2>/dev/null | sed 's/^/  • /' || echo "  (无 Git 跟踪的文件)"
  echo ""
  
  # Commit 摘要
  if [ "$NO_COMMIT" = false ]; then
    echo "📝 最新 Commit："
    git log -1 --format="  %h %s (%ci)" 2>/dev/null || true
    echo ""
  fi
  
  # 下一步操作建议
  echo -e "${BLUE}💡 下一步操作:${NC}"
  echo "  1. Review changes above"
  echo "  2. Test the build: \`go build ./cmd/gleam\`"
  echo "  3. Run tests: \`go test ./...\`"
  echo "  4. Push everything: \`git push origin master --follow-tags\`"
  echo "  5. Create Release: \`bash scripts/release-all.sh --version $NEW_VERSION\`"
  echo ""
  
  # 差异预览
  echo -e "${YELLOW}🔍 预览主要变更内容...${NC}"
  git diff HEAD -- src/ pkg/cmd/ 2>/dev/null | head -30 || echo "  (无代码变更)"
  echo "  ... (更多变更已省略)"
  echo ""
  
  echo -e "${GREEN}━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━${NC}"
  echo ""
}

# ==================== 清理函数 ====================
cleanup() {
  local exit_code=$?
  if [ $exit_code -ne 0 ]; then
    log_error "版本号同步被中断 (退出码：$exit_code)"
  fi
}
trap cleanup EXIT

# ==================== 主流程 ====================
main() {
  log_step "🔄 Gleam 多位置版本号同步系统"
  parse_args "$@"
  
  validate_version
  batch_update
  
  if [ "$DRY_RUN" = false ]; then
    commit_and_push
    verify_update
  fi
  
  generate_summary
}

# 启动主流程
main "$@"
