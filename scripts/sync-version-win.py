#!/usr/bin/env python3
"""
sync-version.py - Windows 兼容版版本号同步工具
使用纯 ASCII 输出，避免编码问题
"""

import argparse
import re
import subprocess
import sys
from datetime import datetime
from pathlib import Path

PROJECT_ROOT = Path(__file__).parent.parent

def log_info(msg):
    print(f"[INFO] {datetime.now().strftime('%H:%M:%S')} {msg}")

def log_success(msg):
    print(f"[SUCCESS] {datetime.now().strftime('%H:%M:%S')} {msg}")

def log_warn(msg):
    print(f"[WARN] {datetime.now().strftime('%H:%M:%S')} {msg}")

def log_error(msg):
    print(f"[ERROR] {datetime.now().strftime('%H:%M:%S')} {msg}", file=sys.stderr)

def log_step(title):
    print("\n" + "=" * 60)
    print("=== " + title + " ===")
    print("=" * 60 + "\n")

def normalize_version(version):
    if not version.startswith('v'):
        return f"v{version}"
    return version

def validate_version(version):
    pattern = r'^v?\d+\.\d+\.\d+$'
    num_part = version[1:] if version.startswith('v') else version
    return bool(re.match(pattern, version)) or bool(re.match(r'\d+\.\d+\.\d+', num_part))

def update_readme(new_version, dry_run=False):
    """更新 README.md"""
    readme_path = PROJECT_ROOT / "README.md"
    if not readme_path.exists():
        log_warn("README.md not found, skipping")
        return
    
    content = readme_path.read_text(encoding='utf-8')
    
    # 替换版本号模式
    patterns_to_replace = [
        (r'# Gleam v\d+\.\d+\.\d+', f'# Gleam {new_version}'),
        (r'(?:download|tag|release)/v\d+\.\d+\.\d+', 
         lambda m: m.group(0).replace(re.search(r'v\d+\.\d+\.\d+', m.group(0)).group(0), new_version)),
        (r'\bv(\d+\.\d+\.\d+)\b', new_version),  # 通用的 vX.Y.Z
    ]
    
    modified = False
    for pattern, replacement in patterns_to_replace:
        def repl_func(m):
            nonlocal modified
            if callable(replacement):
                result = replacement(m)
            else:
                result = replacement
            if result != m.group(0):
                modified = True
            return result
        
        old_content = content
        content = re.sub(pattern, repl_func, content)
    
    if not dry_run and modified:
        readme_path.write_text(content, encoding='utf-8')
        log_success("Updated README.md")
    elif dry_run:
        log_info("Would update README.md")
    else:
        log_info("No changes needed in README.md")

def update_changelog(new_version, dry_run=False):
    """更新 CHANGELOG.md"""
    changelog_path = PROJECT_ROOT / "CHANGELOG.md"
    if not changelog_path.exists():
        log_warn("CHANGELOG.md not found, skipping")
        return
    
    content = changelog_path.read_text(encoding='utf-8')
    
    # 查找现有的版本标题模式
    modified = False
    for pattern in [r'^## v\d+\.\d+\.\d+', r'### v\d+\.\d+\.\d+']:
        if re.search(pattern, content, re.MULTILINE):
            new_content = re.sub(pattern, lambda m: re.sub(r'v\d+\.\d+\.\d+', new_version, m.group(0)), content)
            if new_content != content:
                modified = True
                content = new_content
                break
    
    if not dry_run and modified:
        changelog_path.write_text(content, encoding='utf-8')
        log_success("Updated CHANGELOG.md")
    elif dry_run:
        log_info("Would update CHANGELOG.md")
    else:
        log_info("No changes needed in CHANGELOG.md")

def check_file_exists(filepath):
    """检查文件是否存在"""
    path = PROJECT_ROOT / filepath
    exists = path.exists()
    if exists:
        log_info(f"Found: {filepath}")
    else:
        log_info(f"Not found (optional): {filepath}")
    return exists

def main():
    parser = argparse.ArgumentParser(description='同步版本号到所有位置')
    parser.add_argument('--version', '-v', required=True, help='新版本号（格式：v0.0.34）')
    parser.add_argument('--dry-run', action='store_true', help='模拟运行')
    args = parser.parse_args()
    
    # 验证和规范化版本
    if not validate_version(args.version):
        log_error(f"Invalid version format: {args.version}")
        log_error("Must be vX.Y.Z format (e.g., v0.0.34)")
        sys.exit(1)
    
    new_version = normalize_version(args.version)
    log_step("GLEAM VERSION SYNCHRONIZATION SYSTEM")
    log_info(f"Starting at: {datetime.now().strftime('%Y-%m-%d %H:%M:%S')}")
    
    # 验证版本是否存在重复
    try:
        result = subprocess.run(['git', 'rev-parse', new_version], capture_output=True, timeout=5)
        if result.returncode == 0:
            log_error(f"Tag {new_version} already exists!")
            sys.exit(1)
    except Exception as e:
        log_warn(f"Git check failed: {e}")
    
    log_success(f"Target version: {new_version}")
    
    # 开始批量更新
    log_step("BATCH UPDATE PROCESS")
    
    update_readme(new_version, args.dry_run)
    update_changelog(new_version, args.dry_run)
    
    # 检查 Go 代码文件
    check_file_exists("pkg/buildinfo/version.go")
    
    # 检查其他配置
    check_file_exists("package.json")
    check_file_exists("vscode-gleam/package.json")
    
    log_success(f"All processing complete")
    
    # 生成总结
    print("\n" + "=" * 60)
    print(f"=== VERSION SYNC COMPLETED ===")
    print("=" * 60)
    print(f"\nNew version: {new_version}")
    print(f"Time: {datetime.now().strftime('%Y-%m-%d %H:%M:%S')}")
    print(f"Mode: {'DRY-RUN' if args.dry_run else 'NORMAL'}\n")
    
    if args.dry_run:
        print("[!] This is a simulation run! Files have NOT been modified.")
        print("\nTo apply these changes, run again without --dry-run:")
        print(f"  python scripts/sync-version.py --version {new_version}\n")
    
    print("NEXT STEPS:")
    print("  1. git status      - Check changes")
    print("  2. git add .       - Stage all changes")  
    print("  3. git commit      - Create commit")
    print("  4. git push origin master - Push to remote")
    print("  5. git tag v0.0.34 && git push origin v0.0.34 - Push tag")
    print("  6. bash scripts/release-all.sh --version v0.0.34 - Build & release\n")

if __name__ == '__main__':
    main()
