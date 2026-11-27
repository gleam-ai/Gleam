# Gleam 发布指南

一次完整发布 = 版本号 → 闸门 → CLI 产物 → 桌面安装包 → 上传 → 官网。
每一步都有能 exit non-zero 的命令证明，不靠「应该没问题」。

## 1. 同步版本号（两处，必须一致）

- `internal/buildinfo/buildinfo.go`：`var Version` 与 `var Date`
- `desktop/package.json`：`version`（`desktop/package-lock.json` 的两处 version 一并改）

`scripts/release-all.sh` 开头会校验这两处，不一致直接失败。

## 2. 过闸门

```bash
bash scripts/verify.sh --quick   # 格式 / vet / 编译 / 单测
```

## 3. 构建 CLI（4 平台）

```bash
bash scripts/release-all.sh
```

产物在 `dist/`：带版本号的二进制（`dist/<os>/<arch>/`）、官网用的裸二进制
（`gleam-windows-amd64.exe` 等）、校验和、tar.gz。脚本不上传、不改 git。

## 4. 构建桌面安装包（Windows）

```bash
cd desktop && npm run pack:win
```

产物在系统临时目录 `gleam-pack/`：`Gleam-Setup.exe`、`Gleam-Setup.exe.blockmap`、
`latest.yml`。**三件套缺一不可**——`latest.yml` 是 electron-updater 的更新频道文件，
缺了它应用内「检查更新」直接 404（v0.0.34 的事故）。拷进 `dist/` 再上传。

装机实测的静默安装**只能从 PowerShell 启动安装器**，在 Git Bash 里直接 exec 会装不上并卸掉旧版。
命令与原因见 `.codebuddy/rules/release-workflow.md` 第 6 步（唯一出处）。

## 5. 上传 Release

```bash
git tag v0.0.XX && git push origin master --follow-tags
gh release create v0.0.XX --title "Gleam v0.0.XX" --notes-file <notes.md>
```

然后执行 `release-all.sh` 结尾打印的 `gh release upload ... --clobber` 命令。
上传后验证：

```bash
gh release download v0.0.XX -p latest.yml -O -   # version 必须是新版本
```

## 6. 官网

改 `website/index.html` 与 `website/index-zh.html` 的 `dl-version` / `dl-date`，
push 到 master 后 `.github/workflows/deploy-website.yml` 自动发布，一次推送发两处：
EdgeOne Makers 项目（`gleam.wangjn.top` 的源站）和 GitHub Pages 镜像（`gleam-ai.github.io/Gleam/`）。
EdgeOne 那一步用仓库 Secret `EDGEONE_PAGES_API_TOKEN` 鉴权，缺失时该 job 直接失败。
下载按钮指向 `releases/latest/download/...`，资产名不变就不用改链接。