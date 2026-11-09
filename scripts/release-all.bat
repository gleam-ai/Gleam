@echo off
setlocal enabledelayedexpansion

REM ========================================================================
REM Gleam Multi-Platform Release Script (Windows Batch Version)
REM ========================================================================
REM Usage: scripts\release-all.bat [options]
REM Options:
REM   /version:X.X.X    - Specify version number
REM   /dry-run          - Simulation mode, no actual builds
REM   /no-upload        - Local build only, skip GitHub upload
REM   /skip-verify      - Skip pre-build verification
REM ========================================================================

REM 设置颜色支持
if defined TERM (
    echo This script should be run from Git Bash or WSL on Windows
    exit /b 1
)

REM ==================== 配置区域 ====================
set SCRIPT_DIR=%~dp0
set PROJECT_ROOT=%SCRIPT_DIR%..
set BUILD_TIME=%date% %time%

REM 默认版本（可通过环境变量或参数覆盖）
if not defined VERSION set VERSION=
if not defined GITHUB_TOKEN set GITHUB_TOKEN=

REM 构建目标定义
set TARGETS_WINDOWS_AMD64=windows/amd64
set TARGETS_LINUX_AMD64=linux/amd64
set TARGETS_DARWIN_AMD64=darwin/amd64
set TARGETS_DARWIN_ARM64=darwin/arm64

REM ==================== 工具函数 ====================
echo_info [INFO] %TIME% %*
echo_success [SUCCESS] %TIME% %*
echo_warn [WARN] %TIME% %*
echo_error [ERROR] %TIME% %* & exit /b 1

echo_step ========== %* ==========

==================== 解析命令行参数 ====================
parse_args() {
    while [[ $# -gt 0 ]]; do
        case $1 in
            --version|-v) VERSION="$2"; shift 2 ;;
            --version=* ) VERSION="${1#*=}"; shift ;;
            --dry-run) DRY_RUN=true; shift ;;
            --no-upload) NO_UPLOAD=true; shift ;;
            --skip-verify) SKIP_VERIFY=true; shift ;;
            *) log_error "Unknown option: $1" ;;
        esac
    done
}

==================== 主流程 ====================
main() {
    echo "🚀 Gleam Multi-Platform Release"
    parse_args "$@"
    
    if [ "$DRY_RUN" = true ]; then
        echo "[DRY-RUN] Simulation mode"
        return
    fi
    
    detect_platform
    get_version
    run_build
    generate_checksums
    create_archives
    upload_to_github
}

# 由于这是 Windows，我们使用 Git Bash 执行上面的 shell 脚本
exec_with_bash() {
    bash "$SCRIPT_DIR/release-all.sh" "$@"
}

# 直接调用 git bash 中的脚本
exec_with_bash "$@"
