REM ========================================================================
REM Gleam Release Script for Windows CMD
REM ========================================================================
REM Save as: scripts\release.cmd (not .bat)
REM Run from Git Bash: bash -c "cd scripts && cmd /c release.cmd"
REM ========================================================================

@echo off
setlocal enabledelayedexpansion

echo ╔══════════════════════════════════════════════════════╗
echo ║      Gleam Multi-Platform Release (Windows CMD)     ║
echo ╚══════════════════════════════════════════════════════╝
echo.

REM ==================== 配置区域 ====================
for %%~dp0 do set SCRIPT_DIR=%%~dp0
set PROJECT_ROOT=%SCRIPT_DIR%..
set VERSION_ARG=%1

REM 默认版本号
set VERSION=
if "%VERSION_ARG%"=="" goto GET_VERSION_FROM_GIT

REM 解析 --version 参数
set VERSION=%VERSION_ARG:--version=%
set VERSION=%VERSION:*=~v%
goto START_BUILD

:GetVersionFromGit
echo 正在从 Git tag 获取版本号...
for /f "delims=" %%i in ('git describe --tags --abbrev=0 2^>nul') do set VERSION=%%i
if %ERRORLEVEL% neq 0 goto MANUAL_VERSION

echo 获取到的版本：v%VERSION%
goto START_BUILD

:ManualVersion
echo.
echo [警告] 无法自动获取版本号
timeout /t 2 /nobreak >nul
echo 请输入版本号（例如 v0.0.33）:
set /p VERSION=
echo 最终版本：v!VERSION!

:StartBuild

REM ==================== 环境检测 ====================
echo.
echo 🔍 环境检测
echo ------------------------
where go >nul 2>&1
if %ERRORLEVEL% neq 0 (
    echo [错误] Go 未安装，请先安装 Go 1.22+
    exit /b 1
)

for /f "tokens=3" %%i in ('go version') do set GO_VERSION=%%i
echo Go 版本：%GO_VERSION%

if not exist "%PROJECT_ROOT%\cmd\gleam\main.go" (
    echo [错误] 未找到 cmd/gleam/main.go
    exit /b 1
)
echo ✓ 主程序入口已找到

REM ==================== 构建定义 ====================
echo.
echo 🏗️ 开始构建
echo ------------------------

REM 构建函数
do_build() (
    set GOOS=%~1
    set GOARCH=%~2
    set OUTPUT_NAME=gleam-%VERSION%-!GOOS!-!GOARCH!
    
    if "!GOOS!"=="windows" set OUTPUT_NAME=!OUTPUT_NAME!.exe
    
    echo 构建平台：!GOOS! / !GOARCH!
    cd "%PROJECT_ROOT%\cmd\gleam"
    
    set GOOS=!GOOS!
    set GOARCH=!GOARCH!
    set CGO_ENABLED=0
    
    REM 添加版本信息
    set LDFLAGS=-ldflags="-s -w -X gleam/pkg/buildinfo.Version=%VERSION% -X gleam/pkg/buildinfo.BuildTime=%DATE% %TIME%"
    
    if go build %LDFLAGS% -o "..\..\dist\!GOOS!\!GOARCH!\!OUTPUT_NAME!" .
        echo ✓ !GOOS!!GOARCH! 构建成功
        if "!GOOS!"=="windows" (
            if exist "!OUTPUT_NAME:!.exe" (
                move !OUTPUT_NAME! "!OUTPUT_NAME!.exe" >nul
            )
        )
    else
        echo ✗ !GOOS!!GOARCH! 构建失败
        exit /b 1
    fi
    
    unset GOOS GOARCH CGO_ENABLED
    cd "%SCRIPT_DIR%"..
)

REM 执行各平台构建
cd "%PROJECT_ROOT%\dist"
mkdir windows 2>nul
mkdir linux 2>nul
mkdir darwin 2>nul

echo.
call do_build windows amd64
if %ERRORLEVEL%==0 mkdir windows\amd64 2>nul & move "..\dist\windows\*.exe" "windows\amd64\" 2>nul

echo.
call do_build linux amd64
if %ERRORLEVEL%==0 mkdir linux\amd64 2>nul & move "..\dist\linux\*" "linux\amd64\" 2>nul

echo.
call do_build darwin amd64
if %ERRORLEVEL%==0 mkdir darwin\amd64 2>nul & move "..\dist\darwin\*" "darwin\amd64\" 2>nul

echo.
call do_build darwin arm64
if %ERRORLEVEL%==0 mkdir darwin\arm64 2>nul & move "..\dist\darwin\*" "darwin\arm64\" 2>nul

cd "%SCRIPT_DIR%"..

REM ==================== 生成校验和 ====================
echo.
echo 🔐 生成文件校验和
echo ------------------------
cd "%PROJECT_ROOT%\dist"

powershell -Command "Get-ChildItem -Recurse -File -Include *.exe,*.tar.gz | ForEach-Object { Get-FileHash $_.FullName -Algorithm SHA256 } | Format-Table -AutoSize" >checksums.txt

echo ✓ 校验和已保存到 checksums.txt
cd ..

REM ==================== 打包归档 ====================
echo.
echo 📦 创建归档文件
echo ------------------------

cd "%PROJECT_ROOT%\dist\linux\amd64"
7z a "..\..\Gleam_%VERSION%_linux_amd64.tar.gz" *
cd "%SCRIPT_DIR%"..\..\dist\darwin\amd64
7z a "..\..\Gleam_%VERSION%_darwin_amd64.tar.gz" *
cd "%SCRIPT_DIR%"..\..\dist\darwin\arm64
7z a "..\..\Gleam_%VERSION%_darwin_arm64.tar.gz" *

cd "%SCRIPT_DIR%"..

REM ==================== 总结报告 ====================
echo.
echo ╔══════════════════════════════════════════════════════╗
echo ║            ✅ 构建流程圆满完成                       ║
echo ╚══════════════════════════════════════════════════════╝
echo.
echo 版本信息：v%VERSION%
echo 构建目录：%PROJECT_ROOT%\dist
echo.
echo 📦 构建产物清单:
dir /b "%PROJECT_ROOT%\dist\windows\*.*" 2>nul
dir /b "%PROJECT_ROOT%\dist\linux\*.*" 2>nul
dir /b "%PROJECT_ROOT%\dist\darwin\*.*" 2>nul
echo.
echo 💡 下一步操作:
echo   1. 验证二进制文件可正常运行
echo   2. 更新 CHANGELOG.md
echo   3. 上传到 GitHub Releases:
echo      https://github.com/GoodTime-AI/Gleam/releases/new
echo.

pause
