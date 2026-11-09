#!/usr/bin/env python3
"""
sync-version.py - 一键同步更新所有位置的版本号
Python 实现，更可靠处理特殊字符和 Unicode

Usage: python scripts/sync-version.py --version v0.0.34 [--dry-run] [--no-commit] [--push]
"""

import argparse
import json
import os
import re
import subprocess
import sys
from datetime import datetime, timezone
from pathlib import Path
from typing import Dict, List, Optional, Tuple

# ==================== 颜色输出 ====================
class Colors:
    BLUE = '\033[0;34m'
    GREEN = '\033[0;32m'
    YELLOW = '\033[1;33m'
    RED = '\033[0;31m'
    CYAN = '\033[0;36m'
    NC = '\033[0m'  # No Color

def log_info(msg: str):
    print(f"{Colors.BLUE}[INFO]{Colors.NC} {datetime.now().strftime('%H:%M:%S')} {msg}")

def log_success(msg: str):
    print(f"{Colors.GREEN}[SUCCESS]{Colors.NC} {datetime.now().strftime('%H:%M:%S')} {msg}")

def log_warn(msg: str):
    print(f"{Colors.YELLOW}[WARN]{Colors.NC} {datetime.now().strftime('%H:%M:%S')} {msg}")

def log_error(msg: str):
    print(f"{Colors.RED}[ERROR]{Colors.NC} {datetime.now().strftime('%H:%M:%S')} {msg}", file=sys.stderr)

def log_step(title: str):
    print("\n" + "━" * 60)
    print(f"{Colors.CYAN}★ {title}{Colors.NC}")
    print("━" * 60 + "\n")

# ==================== 配置区域 ====================
SCRIPT_DIR = Path(__file__).parent.resolve()
PROJECT_ROOT = SCRIPT_DIR.parent

# 文件映射：文件名 -> [正则表达式模式列表]
FILE_PATTERNS: Dict[str, List[str]] = {
    # Go 源代码
    "pkg/buildinfo/version.go": [
        r'const Version = "[^"]+"',
        r'var BuildTime = "[^"]+"',
        r'BUILD_TIME := "[^"]+"',
    ],
    
    # 文档文件  
    "README.md": [
        r'# Gleam v\d+\.\d+\.\d+',
        r'\bv\d+\.\d+\.\d+\b',  # 通用的 vX.Y.Z 匹配
        r'(?:download|tag|release)/v\d+\.\d+\.\d+',
    ],
    
    "CHANGELOG.md": [
        r'^## v\d+\.\d+\.\d+',
        r'### v\d+\.\d+\.\d+',
        r'#### v\d+\.\d+\.\d+',
    ],
    
    "docs/RELEASE-NOTES.md": [
        r'Gleam v\d+\.\d+\.\d+',
    ],
    
    # Electron/VSCode 配置
    "package.json": [r'"version":\s*"[^"]+"'],
    "electron-builder.json": [r'"version":\s*"[^"]+"'],
    "vscode-gleam/package.json": [r'"version":\s*"[^"]+"'],
    
    # Docker 配置
    "Dockerfile": [r'gleam:\d+\.\d+\.\d+'],
    "docker-compose.yml": [r'gleam:\d+\.\d+\.\d+'],
}

# ==================== 工具函数 ====================
def parse_args() -> argparse.Namespace:
    parser = argparse.ArgumentParser(
        description='一键同步更新所有位置的版本号',
        formatter_class=argparse.RawDescriptionHelpFormatter,
        epilog="""
示例:
  %(prog)s --version v0.0.34 --commit --push
  %(prog)s v0.0.34 --dry-run
  %(prog)s --version v0.0.35
        """
    )
    parser.add_argument('--version', '-v', required=True,
                       help='新版本号（格式：v0.0.34）')
    parser.add_argument('--dry-run', action='store_true',
                       help='模拟运行，仅显示将要修改的内容')
    parser.add_argument('--no-commit', action='store_true',
                       help='仅修改文件，不创建 Git commit')
    parser.add_argument('--push', action='store_true',
                       help='提交后推送到远程仓库')
    
    return parser.parse_args()

def validate_version(version: str) -> bool:
    """验证版本号格式是否正确"""
    if not re.match(r'^v?\d+\.\d+\.\d+$', version):
        if version.startswith('v'):
            num_version = version[1:]
        else:
            num_version = version
        
        if re.match(r'^\d+\.\d+\.\d+$', num_version):
            return True
        return False
    return True

def normalize_version(version: str) -> str:
    """规范化版本号格式（确保有 'v' 前缀）"""
    if not version.startswith('v'):
        return f"v{version}"
    return version

def get_current_versions() -> Dict[str, str]:
    """从各个来源读取当前版本号"""
    versions = {}
    
    # 从 Go 代码获取
    go_file = PROJECT_ROOT / "pkg/buildinfo/version.go"
    if go_file.exists():
        content = go_file.read_text(encoding='utf-8')
        match = re.search(r'const Version = "([^"]+)"', content)
        if match:
            versions['go_code'] = match.group(1)
    
    # 从 README 获取
    readme_file = PROJECT_ROOT / "README.md"
    if readme_file.exists():
        content = readme_file.read_text(encoding='utf-8')
        match = re.search(r'# Gleam (v[\d.]+)', content)
        if match:
            versions['readme'] = match.group(1)
    
    # 从 Git tag 获取
    try:
        result = subprocess.run(
            ['git', 'describe', '--tags', '--abbrev=0'],
            cwd=str(PROJECT_ROOT),
            capture_output=True, text=True, timeout=5
        )
        if result.returncode == 0:
            versions['git_tag'] = result.stdout.strip()
    except:
        versions['git_tag'] = 'none'
    
    return versions

# ==================== 核心更新逻辑 ====================
class VersionSyncer:
    def __init__(self, new_version: str, dry_run: bool = False):
        self.new_version = normalize_version(new_version)
        self.dry_run = dry_run
        self.modified_files: List[str] = []
        
    def update_file_content(self, old_content: str) -> str:
        """根据所有文件的模式进行版本替换"""
        new_content = old_content
        
        for pattern in FILE_PATTERNS.get('', []):
            new_content = re.sub(pattern, 
                                lambda m: self._replace_in_match(m, pattern),
                                new_content, flags=re.MULTILINE)
        
        return new_content
    
    def _replace_in_match(self, match: re.Match, pattern: str) -> str:
        """在匹配处进行替换的核心逻辑"""
        matched_text = match.group(0)
        
        # 针对不同模式的特殊处理
        if 'Version =' in matched_text or 'VERSION' in pattern:
            return matched_text.replace(match.group(0).split('=')[1].strip(), f'"{self.new_version}"')
        
        elif 'BuildTime' in matched_text or 'BUILD_TIME' in pattern:
            # 更新时间戳
            now = datetime.now(timezone.utc).strftime('%Y-%m-%dT%H:%M:%SZ')
            return re.sub(r'"[^"]+"', f'"{now}"', matched_text)
        
        elif '## v' in matched_text or '### v' in matched_text:
            # CHANGELOG 标题
            return re.sub(r'v\d+\.\d+\.\d+', self.new_version, matched_text)
        
        elif '# Gleam v' in matched_text:
            # README 标题
            return matched_text.replace(re.search(r'v\d+\.\d+\.\d+', matched_text).group(0), 
                                       self.new_version)
        
        else:
            # 通用替换：将 vX.Y.Z 替换为新版本
            return re.sub(r'v\d+\.\d+\.\d+', self.new_version, matched_text)
    
    def process_all_files(self) -> None:
        """处理所有需要更新的文件"""
        log_step("开始批量更新版本号")
        
        updated_count = 0
        skipped_count = 0
        
        # 核心文件
        core_files = ["README.md", "CHANGELOG.md"]
        
        for filename in core_files:
            filepath = PROJECT_ROOT / filename
            if filepath.exists():
                log_info(f"处理：{filename}")
                self._update_single_file(filepath)
                updated_count += 1
            else:
                log_warn(f"跳过（文件不存在）：{filename}")
                skipped_count += 1
        
        # 检查 Go 版本文件
        go_file = PROJECT_ROOT / "pkg/buildinfo/version.go"
        if go_file.exists():
            log_info("处理：pkg/buildinfo/version.go")
            self._update_single_file(go_file)
            updated_count += 1
        
        # 其他文件
        for filename, patterns in FILE_PATTERNS.items():
            if filename in core_files or filename == "pkg/buildinfo/version.go":
                continue
                
            filepath = PROJECT_ROOT / filename
            if filepath.exists():
                log_info(f"处理：{filename}")
                self._update_single_file(filepath)
                updated_count += 1
        
        log_success(f"更新了 {updated_count} 个文件，跳过了 {skipped_count} 个")
    
    def _update_single_file(self, filepath: Path) -> None:
        """更新单个文件"""
        try:
            original_content = filepath.read_text(encoding='utf-8')
            new_content = self.update_file_content(original_content)
            
            if new_content != original_content:
                if self.dry_run:
                    log_info(f"  ✓ 内容已变更（未实际写入，dry-run 模式）")
                    # 显示差异
                    diff_preview = self._get_diff_preview(original_content, new_content)
                    if diff_preview:
                        log_info(f"  预览：{diff_preview[:100]}...")
                else:
                    filepath.write_text(new_content, encoding='utf-8')
                    self.modified_files.append(str(filepath.relative_to(PROJECT_ROOT)))
                    log_success(f"  ✓ 更新成功")
            else:
                log_info(f"  - 内容无需更改")
                
        except Exception as e:
            log_error(f"  ✗ 更新失败：{e}")
    
    def _get_diff_preview(self, old: str, new: str) -> str:
        """获取差异预览（简化版）"""
        old_lines = old.splitlines()
        new_lines = new.splitlines()
        
        changes = []
        for i, (old_line, new_line) in enumerate(zip(old_lines, new_lines)):
            if old_line != new_line:
                changes.append(f"行{i+1}: {old_line[:50]}... → {new_line[:50]}")
                if len(changes) >= 3:
                    break
        
        return "; ".join(changes)
    
    def check_github_cli(self) -> bool:
        """检查 GitHub CLI 是否安装"""
        try:
            result = subprocess.run(
                ['gh', '--version'],
                capture_output=True, text=True, timeout=5
            )
            return result.returncode == 0
        except:
            return False
    
    def verify_update(self) -> bool:
        """验证更新结果"""
        log_step("验证版本号更新一致性")
        
        all_good = True
        
        # 检查 README
        readme = PROJECT_ROOT / "README.md"
        if readme.exists():
            content = readme.read_text(encoding='utf-8')
            if self.new_version in content:
                log_success("✓ README.md 包含新版本号")
            else:
                log_warn("⚠ README.md 可能缺少版本号更新")
                all_good = False
        
        # 检查 CHANGELOG
        changelog = PROJECT_ROOT / "CHANGELOG.md"
        if changelog.exists():
            content = changelog.read_text(encoding='utf-8')
            if f"## {self.new_version}" in content or self.new_version in content:
                log_success("✓ CHANGELOG.md 包含新版本号")
            else:
                log_warn("⚠ CHANGELOG.md 可能需要手动添加")
        
        return all_good
    
    def create_commit(self) -> bool:
        """创建 Git commit"""
        if self.dry_run:
            log_info("跳过 Git commit (--dry-run)")
            return True
        
        # 检查是否有更改
        status = subprocess.run(
            ['git', 'status', '--porcelain'],
            cwd=str(PROJECT_ROOT),
            capture_output=True, text=True
        )
        
        if not status.stdout.strip():
            log_info("没有文件需要提交")
            return True
        
        # 配置 Git
        subprocess.run(['git', 'config', 'user.name', 'Release Bot'], 
                      cwd=str(PROJECT_ROOT))
        subprocess.run(['git', 'config', 'user.email', 'noreply@gleam.ai'],
                      cwd=str(PROJECT_ROOT))
        
        # 添加并提交
        subprocess.run(['git', 'add', '.'], cwd=str(PROJECT_ROOT))
        
        commit_msg = f"""chore(release): {self.new_version}

- Synced version across all files (README, CHANGELOG, configs)
- Updated build info and timestamps"""
        
        result = subprocess.run(
            ['git', 'commit', '-m', commit_msg],
            cwd=str(PROJECT_ROOT),
            capture_output=True, text=True
        )
        
        if result.returncode == 0:
            log_success(f"Created commit: {result.stdout.strip()}")
            return True
        else:
            log_error(f"Commit failed: {result.stderr}")
            return False
    
    def push_changes(self) -> bool:
        """推送更改到远程仓库"""
        if not args.push:
            log_info("跳过推送 (--no-push)")
            return True
        
        log_step("推送到远程仓库")
        
        # 推送主分支
        result = subprocess.run(
            ['git', 'push', 'origin', 'HEAD'],
            cwd=str(PROJECT_ROOT),
            capture_output=True, text=True
        )
        
        if result.returncode != 0:
            log_warn(f"Push warning: {result.stderr[:200]}")
        
        # 创建标签
        tag_result = subprocess.run(
            ['git', 'tag', self.new_version],
            cwd=str(PROJECT_ROOT),
            capture_output=True, text=True
        )
        
        if tag_result.returncode == 0:
            log_success(f"Tag created: {self.new_version}")
            
            # 推送标签
            push_tag = subprocess.run(
                ['git', 'push', 'origin', self.new_version],
                cwd=str(PROJECT_ROOT),
                capture_output=True, text=True
            )
            
            if push_tag.returncode == 0:
                log_success(f"Tag pushed successfully")
            else:
                log_warn(f"Tag push failed: {push_tag.stderr[:200]}")
        
        return True
    
    def generate_summary(self) -> None:
        """生成最终报告"""
        print("\n" + "━" * 60)
        print(f"{Colors.GREEN}╔═══════════════════════════════════════════════════════╗{Colors.NC}")
        print(f"{Colors.GREEN}║           ✅ 版本号同步圆满完成                       ║{Colors.NC}")
        print(f"{Colors.GREEN}╚═══════════════════════════════════════════════════════╝{Colors.NC}")
        print("\n" + f"{Colors.CYAN}新版本:{Colors.NC}       {self.new_version}")
        print(f"{Colors.CYAN}执行时间:{Colors.NC}     {datetime.now().strftime('%Y-%m-%d %H:%M:%S')}")
        print(f"{Colors.CYAN}操作模式:{Colors.NC}     {'Dry-run' if self.dry_run else 'Normal'}")
        
        if self.modified_files:
            print(f"\n{Colors.YELLOW}📁 已修改的文件 ({len(self.modified_files)}){Colors.NC}:")
            for f in self.modified_files[:10]:  # 只显示前 10 个
                print(f"  • {f}")
            if len(self.modified_files) > 10:
                print(f"  ... 还有 {len(self.modified_files) - 10} 个文件")
        
        print(f"\n{Colors.BLUE}💡 下一步操作:{Colors.NC}")
        print("  1. git status - 检查更改")
        print("  2. git add . && git commit -m 'chore(release): ...'")
        print("  3. git push origin master")
        print("  4. git push origin v0.0.34")
        print("  5. bash scripts/release-all.sh --version v0.0.34")
        print("")
        
        if self.dry_run:
            print(f"{Colors.YELLOW}⚠️  这是模拟运行！文件尚未修改.{Colors.NC}\n")
            print("要实际应用这些更改，请再次运行（去掉 --dry-run）：\n")
            print(f"  {sys.argv[0]} --version {self.new_version} --commit")

# ==================== 主流程 ====================
if __name__ == '__main__':
    args = parse_args()
    
    log_step(f"Gleam 多位置版本号同步系统")
    log_info(f"启动时间：{datetime.now().strftime('%Y-%m-%d %H:%M:%S')}")
    
    # 验证版本
    if not validate_version(args.version):
        log_error(f"无效的版本号格式：{args.version}")
        log_error("必须使用 vX.Y.Z 格式（例如 v0.0.34）")
        sys.exit(1)
    
    new_ver = normalize_version(args.version)
    
    # 检查重复
    try:
        result = subprocess.run(
            ['git', 'rev-parse', new_ver],
            cwd=str(PROJECT_ROOT),
            capture_output=True, text=True, timeout=5
        )
        if result.returncode == 0:
            log_error(f"Tag {new_ver} 已存在！")
            sys.exit(1)
    except:
        pass
    
    # 初始化同步器
    syncer = VersionSyncer(new_ver, args.dry_run)
    
    # 执行同步
    syncer.process_all_files()
    
    # 验证结果
    if not args.dry_run:
        syncer.verify_update()
    
    # 创建 commit
    if not args.dry_run and not args.no_commit:
        syncer.create_commit()
        syncer.push_changes()
    
    # 生成报告
    syncer.generate_summary()
