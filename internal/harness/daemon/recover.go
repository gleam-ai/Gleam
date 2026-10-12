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

type RecoverFunc func(task *QueuedTask) error

type RecoverOptions struct {
	MaxConcurrent int
	OnProgress    func(recovered, total int)
	Timeout       time.Duration
}

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

	resetCount, err := queue.RecoverPending()
	if err != nil {
		return 0, 0, fmt.Errorf("重置运行中任务失败：%w", err)
	}
	if resetCount > 0 {
		log.Printf("[daemon] 重置了 %d 个运行中任务为待执行（进程重启恢复）", resetCount)
	}

	pending, err := queue.ListPending()
	if err != nil {
		return 0, 0, fmt.Errorf("列出待执行任务失败：%w", err)
	}

	total := len(pending)
	if total == 0 {
		return 0, 0, nil
	}

	log.Printf("[daemon] 开始恢复 %d 个待执行任务", total)

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

	for i := 0; i < opts.MaxConcurrent; i++ {
		sem <- struct{}{}
	}

	return doneCount, failCount, nil
}

func recoverTask(
	ctx context.Context,
	queue *Queue,
	agent *agent.Agent,
	sched *scheduler.Scheduler,
	task *QueuedTask,
) error {
	queued, err := queue.Dequeue()
	if err != nil {
		return fmt.Errorf("从队列取出任务失败：%w", err)
	}
	if queued == nil {
		return fmt.Errorf("任务 %s 已被其他进程取出", task.ID)
	}

	var result *types.GoalResult
	if sched != nil && queued.Mode == "scheduled" {
		// TODO: restore scheduled jobs
		result = agent.RunGoal(ctx, types.GoalRequest{
			Goal: queued.Goal,
			Mode: queued.Mode,
		})
	} else {
		result = agent.RunGoal(ctx, types.GoalRequest{
			Goal: queued.Goal,
			Mode: queued.Mode,
		})
	}

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
