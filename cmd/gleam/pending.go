package main

import (
	"flag"
	"fmt"
	"path/filepath"
	"strings"
	"time"

	"gleam/internal/config"
	"gleam/internal/harness/safety"
)

const pendingFile = "pending_approvals.json"

const auditFile = "audit.jsonl"

func cmdPending(args []string) error {
	fs := flag.NewFlagSet("pending", flag.ContinueOnError)
	configPath := fs.String("config", "", "配置文件路径")
	dataDir := fs.String("data-dir", "", "数据目录")
	clear := fs.Bool("clear", false, "清理全部等待审批记录（确认已重新提交后使用）")
	audit := fs.Bool("audit", false, "改为列出审计留痕（最近 50 条，落盘的 audit.jsonl）")
	egressOnly := fs.Bool("egress", false, "只列出数据出网留痕（配合 --audit：提示词发给了哪个模型服务、web.fetch 抓了哪个站点）")
	flagArgs, positionals := splitFlagArgs(args, fs)
	if err := fs.Parse(flagArgs); err != nil {
		return err
	}
	if len(positionals) > 0 {
		return fmt.Errorf("pending 不接受位置参数 %q", positionals[0])
	}

	dir, err := resolveDataDir(*configPath, *dataDir)
	if err != nil {
		return err
	}

	if *audit {
		limit := 50
		if *egressOnly {
			limit = 0
		}
		entries, err := safety.LoadAuditLog(filepath.Join(dir, auditFile), limit)
		if err != nil {
			return err
		}
		if *egressOnly {
			kept := make([]safety.AuditEntry, 0, len(entries))
			for _, e := range entries {
				if e.Action == "egress" {
					kept = append(kept, e)
				}
			}
			entries = kept
			if len(entries) > 50 {
				entries = entries[:50]
			}
		}
		if len(entries) == 0 {
			if *egressOnly {
				fmt.Println("没有数据出网留痕（模型调用与 web.fetch 会自动留痕到 " + filepath.Join(dir, auditFile) + "）")
				return nil
			}
			fmt.Println("没有审计记录（需要审批的动作与被拦动作会自动留痕到 " + filepath.Join(dir, auditFile) + "）")
			return nil
		}
		fmt.Printf("审计留痕（最近 %d 条，新的在前）\n\n", len(entries))
		for _, e := range entries {
			fmt.Printf("  %s  %-14s %-6s %-8s %s\n",
				e.Time.Format("01-02 15:04:05"), e.Tool, e.Risk, e.Action, firstLineOr(e.Reason))
			if e.Detail != "" {
				fmt.Printf("             %s\n", firstLineOr(e.Detail))
			}
		}
		return nil
	}

	path := filepath.Join(dir, pendingFile)
	if *clear {
		if err := safety.ClearPendingFile(path); err != nil {
			return err
		}
		fmt.Println("已清理等待审批记录")
		return nil
	}

	items, err := safety.LoadPendingApprovals(path)
	if err != nil {
		return err
	}
	if len(items) == 0 {
		fmt.Println("没有等待审批的任务")
		return nil
	}
	fmt.Printf("等待审批的任务（%d 条）——进程若已退出，请人工确认后重新提交\n\n", len(items))
	for _, p := range items {
		ago := time.Since(p.At).Round(time.Second)
		if ago < 0 {
			ago = 0
		}
		fmt.Printf("  任务 %s  步骤 %s\n", p.TaskID, p.StepID)
		fmt.Printf("    工具 %s（风险 %s）等待 %v\n", p.Tool, p.Risk, ago)
		if p.Reason != "" {
			fmt.Printf("    原因：%s\n", p.Reason)
		}
	}
	fmt.Printf("\n确认已重新提交后，可执行 gleam pending --clear 清理。\n")
	return nil
}

func resolveDataDir(configPath, dataDir string) (string, error) {
	if strings.TrimSpace(dataDir) != "" {
		return dataDir, nil
	}
	cfg, err := config.Load(configPath)
	if err != nil {
		return "", err
	}
	return cfg.DataDir, nil
}

func firstLineOr(s string) string {
	s = strings.TrimSpace(strings.SplitN(s, "\n", 2)[0])
	if s == "" {
		return "-"
	}
	if r := []rune(s); len(r) > 60 {
		return string(r[:60]) + "…"
	}
	return s
}
