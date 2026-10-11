package main

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
)

func stateReport(dataDir string) string {
	var b strings.Builder
	fmt.Fprintf(&b, "\n状态落盘（四类：配置 / 会话 / 记忆 / 任务）  数据目录 %s\n", dataDir)
	fmt.Fprintf(&b, "  只报位置与体量，不校验内容——缺目录是常态（没用过就没有），不是不合格\n")
	fmt.Fprintf(&b, "  数据目录之外的条目按**当前目录**解析（配置的主文件就是这么找的），换目录跑会显示 [无]\n")

	for _, c := range stateCategories {
		fmt.Fprintf(&b, "\n  %s · %s\n", c.Name, c.Meaning)
		fmt.Fprintf(&b, "      谁能改：%s\n", c.Who)
		for _, e := range stateEntries {
			if e.Category != c.Name {
				continue
			}
			fmt.Fprintf(&b, "      %-28s %s · %s\n", e.Path, describeStatePath(dataDir, e), e.Role)
		}
	}
	return b.String()
}

type stateCategory struct {
	Name    string
	Meaning string
	Who     string
}

var stateCategories = []stateCategory{
	{
		Name:    "配置",
		Meaning: `决定"怎么跑"：模型、档位、安全姿态、工作区`,
		Who:     "Web UI 设置页 / GLEAM_* 环境变量 / 手改 yaml",
	},
	{
		Name:    "会话",
		Meaning: "说过什么：一轮轮对话与它们的滚动上下文",
		Who:     "对话本身（自动落盘）/ Web UI 会话列表",
	},
	{
		Name:    "记忆",
		Meaning: "跨会话攒下来的：长期记忆与成长日志",
		Who:     "memory.save / memory.delete（经审批）/ Web UI 记忆页",
	},
	{
		Name:    "任务",
		Meaning: "每一次执行留下的痕迹",
		Who:     "引擎（自动落盘）/ gleam pending --clear",
	},
	{
		Name:    "资产",
		Meaning: "固化下来的能力与片段（四类之外，见设计文档 §4.6.26）",
		Who:     "技能市场 / skills/save / 片段工具",
	},
}

type stateEntry struct {
	Category string
	Path     string
	Role     string
	Outside  bool
}

var stateEntries = []stateEntry{
	{"配置", "settings.yaml", "设置覆盖层（UI 保存即热生效；删掉就回到主配置）", false},
	{"配置", "credentials.json", "模型凭证（与覆盖层分开存，免得随设置一起被导出）", false},
	{"配置", "configs/config.yaml", "主配置（默认找当前目录下的这个文件；--config 可指定）", true},

	{"会话", "conversations", "每个会话一个 json（消息、时间、用量）", false},
	{"会话", "spaces", "微光空间：按工作文件夹分组会话；state.json 记当前空间", false},
	{"会话", "memory/context.json", "滚动上下文与压缩状态（住在 memory/ 下，但它是会话状态）", false},

	{"记忆", "memory/longterm.json", "长期记忆（容量淘汰 + 相似度判重 + 软删）", false},
	{"记忆", "growth.json", "成长日志：任务统计与等级（只增，用于跨会话迭代）", false},

	{"任务", "tasks", "任务终态记录（回放 / 归因 / 质量统计的分母来源）", false},
	{"任务", "runs", "运行中日志：每步追加一行，正常结束即删（所以空是常态）", false},
	{"任务", "replays", "回放与分叉产物（刻意不进 tasks/，否则污染统计分母）", false},
	{"任务", "schedules.json", "定时任务：Cron / 间隔 / 触发条件 + 通知策略", false},
	{"任务", "pending_approvals.json", "等待审批的任务（进程退出后重启仍能列出）", false},
	{"任务", "audit.jsonl", "全量审计：追加式，含自动放行与数据出网留痕", false},
	{"任务", "tool-output", "超长工具输出的落盘缓存（按任务数裁剪，不是归档）", false},
	{"任务", "snapshots", "写前快照：改动清单能还原的依据（按任务数裁剪，含原文，权限 0600）", false},
	{"任务", "cues.json", "候补目标的处置记录（别再提 / 已采纳，含目标原文，权限 0600）；卡片本身不落盘，由任务历史现算", false},
	{"任务", "geo_history.json", "GEO 生成记录（生成式引擎优化板块的历史）", false},

	{"资产", "skills", "固化的工作流（YAML，同名再存版本号自增）", false},
	{"资产", "snippets.json", "文本片段（支持变量展开）", false},
}

func describeStatePath(dataDir string, e stateEntry) string {
	root := dataDir
	if e.Outside {
		root = "."
	}
	full := filepath.Join(root, filepath.FromSlash(e.Path))
	info, err := os.Stat(full)
	if err != nil {
		return "[无]"
	}
	if !info.IsDir() {
		return humanBytes(info.Size())
	}
	n, bytes, err := countTree(full)
	if err != nil {
		return "[读不到]"
	}
	return fmt.Sprintf("%d 个文件 · %s", n, humanBytes(bytes))
}

func countTree(dir string) (int, int64, error) {
	var n int
	var bytes int64
	err := filepath.WalkDir(dir, func(_ string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if d.IsDir() {
			return nil
		}
		n++
		if info, ierr := d.Info(); ierr == nil {
			bytes += info.Size()
		}
		return nil
	})
	return n, bytes, err
}

func humanBytes(n int64) string {
	const unit = 1024
	if n < unit {
		return fmt.Sprintf("%d B", n)
	}
	value := float64(n)
	units := []string{"KB", "MB", "GB"}
	for i, u := range units {
		value /= unit
		if value < unit || i == len(units)-1 {
			return fmt.Sprintf("%.1f %s", value, u)
		}
	}
	return fmt.Sprintf("%d B", n)
}
