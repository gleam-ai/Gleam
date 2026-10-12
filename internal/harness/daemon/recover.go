package daemon

import (
	"context"
	"fmt"
	"log"
	"sync"
	"time"

	"gleam/internal/agent"
	"gleam/internal/harness/scheduler"
	"gleam/pkg/types"
)

// RecoverFunc 恢复任务的回调类型。
type RecoverFunc func(task *QueuedTask) error

// RecoverOptions 恢复选项。
type RecoverOptions struct {
	// MaxConcurrent 最大并发恢复任务数（默认 1）。
	MaxConcurrent int
	// OnProgress 恢复进度回调。
	OnProgress func(recovered, total int)
	// Timeout 单个任务恢复超时（默认 30 分钟）。
	Timeout time.Duration
}

// Recover 恢复未完成任务（进程重启后调用）。
//
// 恢复策略：
// 1. 将所有 running 状态的任务重置为 pending（进程崩溃导致）
// 2. 按优先级和创建时间排序，逐个重新提交给 Agent
// 3. 恢复过程中保持队列更新（完成一个再取下一个）
func Recover(
	ctx context.Context,
	queue *Queue,
	agent *agent.Agent,
	sched *scheduler.Scheduler,
	opts RecoverOptions,
) (recovered int, failed int, err error) {
	if opts.MaxConcurrent <= 0 {
		opts.MaxConcurrent = 1
	}
	if opts.Timeout == 0 {
		opts.Timeout = 30 * time.Minute
	}

	// 第一步：重置所有 running 任务为 pending
	resetCount, err := queue.RecoverPending()
	if err != nil {
		return 0, 0, fmt.Errorf("重置运行中任务失败：%w", err)
	}
	if resetCount > 0 {
		log.Printf("[daemon] 重置了 %d 个运行中任务为待执行（进程重启恢复）", resetCount)
	}

	// 第二步：获取所有 pending 任务
	pending, err := queue.ListPending()
	if err != nil {
		return 0, 0, fmt.Errorf("列出待执行任务失败：%w", err)
	}

	total := len(pending)
	if total == 0 {
		return 0, 0, nil
	}

	log.Printf("[daemon] 开始恢复 %d 个待执行任务", total)

	// 第三步：并发恢复任务
	sem := make(chan struct{}, opts.MaxConcurrent)
	var doneCount int
	var failCount int
	var mu sync.Mutex

	for _, task := range pending {
		select {
		case <-ctx.Done():
			return doneCount, failCount, ctx.Err()
		default:
		}

		sem <- struct{}{}
		go func(t *QueuedTask) {
			defer func() { <-sem }()

			taskCtx, cancel := context.WithTimeout(ctx, opts.Timeout)
			defer cancel()

			err := recoverTask(taskCtx, queue, agent, sched, t)
			mu.Lock()
			if err != nil {
				failCount++
				log.Printf("[daemon] 任务 %s 恢复失败：%v", t.ID, err)
			} else {
				doneCount++
				log.Printf("[daemon] 任务 %s 恢复成功", t.ID)
			}
			mu.Unlock()

			if opts.OnProgress != nil {
				opts.OnProgress(doneCount+failCount, total)
			}
		}(task)
	}

	// 等待所有任务完成
	for i := 0; i < opts.MaxConcurrent; i++ {
		sem <- struct{}{}
	}

	return doneCount, failCount, nil
}

// recoverTask 恢复单个任务。
func recoverTask(
	ctx context.Context,
	queue *Queue,
	agent *agent.Agent,
	sched *scheduler.Scheduler,
	task *QueuedTask,
) error {
	// 从队列取出（标记为 running）
	queued, err := queue.Dequeue()
	if err != nil {
		return fmt.Errorf("从队列取出任务失败：%w", err)
	}
	if queued == nil {
		return fmt.Errorf("任务 %s 已被其他进程取出", task.ID)
	}

	// 执行任务
	var result *types.GoalResult
	if sched != nil && queued.Mode == "scheduled" {
		// 定时任务：通过调度器执行
		// TODO: 实现定时任务恢复
		result = agent.RunGoal(ctx, types.GoalRequest{
			Goal: queued.Goal,
			Mode: queued.Mode,
		})
	} else {
		// 普通任务：直接执行
		result = agent.RunGoal(ctx, types.GoalRequest{
			Goal: queued.Goal,
			Mode: queued.Mode,
		})
	}

	// 更新队列状态
	if result.Status == types.GoalSuccess {
		if err := queue.Complete(queued.ID, result); err != nil {
			return fmt.Errorf("标记任务完成失败：%w", err)
		}
	} else {
		errMsg := fmt.Sprintf("%s: %s", result.Status, result.Error)
		if err := queue.Fail(queued.ID, errMsg); err != nil {
			return fmt.Errorf("标记任务失败失败：%w", err)
		}
		return fmt.Errorf("任务执行失败：%s", errMsg)
	}

	return nil
}
