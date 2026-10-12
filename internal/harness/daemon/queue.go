package daemon

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"

	_ "modernc.org/sqlite"
)

// TaskState 任务状态。
type TaskState string

const (
	TaskPending   TaskState = "pending"
	TaskRunning   TaskState = "running"
	TaskCompleted TaskState = "completed"
	TaskFailed    TaskState = "failed"
	TaskCancelled TaskState = "cancelled"
)

// QueuedTask 队列中的任务。
type QueuedTask struct {
	ID        string    `json:"id"`
	Goal      string    `json:"goal"`
	Mode      string    `json:"mode"`
	State     TaskState `json:"state"`
	Priority  int       `json:"priority"` // 0=normal, 1=high, 2=urgent
	CreatedAt time.Time `json:"created_at"`
	StartedAt *time.Time `json:"started_at,omitempty"`
	EndedAt   *time.Time `json:"ended_at,omitempty"`
	Error     string    `json:"error,omitempty"`
	Result    string    `json:"result,omitempty"` // JSON 序列化的 GoalResult
}

// Queue 任务队列（SQLite 持久化）。
type Queue struct {
	mu sync.Mutex
	db *sql.DB
}

// OpenQueue 打开任务队列（文件不存在则创建）。
func OpenQueue(dataDir string) (*Queue, error) {
	if err := os.MkdirAll(dataDir, 0o755); err != nil {
		return nil, err
	}

	dbPath := filepath.Join(dataDir, "daemon_queue.db")
	db, err := sql.Open("sqlite", dbPath+"?_journal_mode=WAL&_busy_timeout=5000")
	if err != nil {
		return nil, fmt.Errorf("打开任务队列失败：%w", err)
	}

	// 创建表
	if err := initSchema(db); err != nil {
		db.Close()
		return nil, fmt.Errorf("初始化队列 schema 失败：%w", err)
	}

	return &Queue{db: db}, nil
}

// Close 关闭队列。
func (q *Queue) Close() error {
	return q.db.Close()
}

// Enqueue 添加任务到队列。
func (q *Queue) Enqueue(task *QueuedTask) error {
	q.mu.Lock()
	defer q.mu.Unlock()

	if task.ID == "" {
		return fmt.Errorf("任务 ID 不能为空")
	}
	if task.Goal == "" {
		return fmt.Errorf("任务目标不能为空")
	}
	if task.State == "" {
		task.State = TaskPending
	}
	if task.CreatedAt.IsZero() {
		task.CreatedAt = time.Now()
	}

	_, err := q.db.Exec(`
		INSERT INTO tasks (id, goal, mode, state, priority, created_at)
		VALUES (?, ?, ?, ?, ?, ?)
	`, task.ID, task.Goal, task.Mode, task.State, task.Priority, task.CreatedAt)

	return err
}

// Dequeue 取出下一个待执行任务（按优先级和创建时间排序）。
func (q *Queue) Dequeue() (*QueuedTask, error) {
	q.mu.Lock()
	defer q.mu.Unlock()

	task := &QueuedTask{}
	err := q.db.QueryRow(`
		UPDATE tasks
		SET state = ?, started_at = ?
		WHERE id = (
			SELECT id FROM tasks
			WHERE state = ?
			ORDER BY priority DESC, created_at ASC
			LIMIT 1
		)
		RETURNING id, goal, mode, state, priority, created_at, started_at
	`, TaskRunning, time.Now(), TaskPending).Scan(
		&task.ID, &task.Goal, &task.Mode, &task.State, &task.Priority,
		&task.CreatedAt, &task.StartedAt,
	)

	if err == sql.ErrNoRows {
		return nil, nil // 队列为空
	}
	if err != nil {
		return nil, err
	}

	return task, nil
}

// Complete 标记任务完成。
func (q *Queue) Complete(id string, result interface{}) error {
	q.mu.Lock()
	defer q.mu.Unlock()

	var resultJSON string
	if result != nil {
		data, err := json.Marshal(result)
		if err != nil {
			return err
		}
		resultJSON = string(data)
	}

	now := time.Now()
	_, err := q.db.Exec(`
		UPDATE tasks
		SET state = ?, ended_at = ?, result = ?
		WHERE id = ?
	`, TaskCompleted, now, resultJSON, id)

	return err
}

// Fail 标记任务失败。
func (q *Queue) Fail(id string, errMsg string) error {
	q.mu.Lock()
	defer q.mu.Unlock()

	now := time.Now()
	_, err := q.db.Exec(`
		UPDATE tasks
		SET state = ?, ended_at = ?, error = ?
		WHERE id = ?
	`, TaskFailed, now, errMsg, id)

	return err
}

// Cancel 取消任务。
func (q *Queue) Cancel(id string) error {
	q.mu.Lock()
	defer q.mu.Unlock()

	_, err := q.db.Exec(`
		UPDATE tasks
		SET state = ?, ended_at = ?
		WHERE id = ? AND state IN (?, ?)
	`, TaskCancelled, time.Now(), id, TaskPending, TaskRunning)

	return err
}

// ListPending 列出所有待执行任务。
func (q *Queue) ListPending() ([]*QueuedTask, error) {
	q.mu.Lock()
	defer q.mu.Unlock()

	rows, err := q.db.Query(`
		SELECT id, goal, mode, state, priority, created_at, started_at
		FROM tasks
		WHERE state = ?
		ORDER BY priority DESC, created_at ASC
	`, TaskPending)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var tasks []*QueuedTask
	for rows.Next() {
		t := &QueuedTask{}
		if err := rows.Scan(&t.ID, &t.Goal, &t.Mode, &t.State, &t.Priority, &t.CreatedAt, &t.StartedAt); err != nil {
			return nil, err
		}
		tasks = append(tasks, t)
	}

	return tasks, rows.Err()
}

// ListRunning 列出所有运行中任务。
func (q *Queue) ListRunning() ([]*QueuedTask, error) {
	q.mu.Lock()
	defer q.mu.Unlock()

	rows, err := q.db.Query(`
		SELECT id, goal, mode, state, priority, created_at, started_at
		FROM tasks
		WHERE state = ?
		ORDER BY started_at ASC
	`, TaskRunning)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var tasks []*QueuedTask
	for rows.Next() {
		t := &QueuedTask{}
		if err := rows.Scan(&t.ID, &t.Goal, &t.Mode, &t.State, &t.Priority, &t.CreatedAt, &t.StartedAt); err != nil {
			return nil, err
		}
		tasks = append(tasks, t)
	}

	return tasks, rows.Err()
}

// RecoverPending 恢复所有运行中任务为待执行（进程重启后调用）。
func (q *Queue) RecoverPending() (int, error) {
	q.mu.Lock()
	defer q.mu.Unlock()

	result, err := q.db.Exec(`
		UPDATE tasks
		SET state = ?, started_at = NULL
		WHERE state = ?
	`, TaskPending, TaskRunning)
	if err != nil {
		return 0, err
	}

	affected, err := result.RowsAffected()
	return int(affected), err
}

// CleanupOld 清理已完成/失败的任务（保留最近 N 天）。
func (q *Queue) CleanupOld(keepDays int) (int, error) {
	q.mu.Lock()
	defer q.mu.Unlock()

	cutoff := time.Now().AddDate(0, 0, -keepDays)
	result, err := q.db.Exec(`
		DELETE FROM tasks
		WHERE state IN (?, ?, ?) AND ended_at < ?
	`, TaskCompleted, TaskFailed, TaskCancelled, cutoff)
	if err != nil {
		return 0, err
	}

	affected, err := result.RowsAffected()
	return int(affected), err
}

// Stats 队列统计。
type Stats struct {
	Pending   int `json:"pending"`
	Running   int `json:"running"`
	Completed int `json:"completed"`
	Failed    int `json:"failed"`
	Total     int `json:"total"`
}

// GetStats 获取队列统计。
func (q *Queue) GetStats() (*Stats, error) {
	q.mu.Lock()
	defer q.mu.Unlock()

	stats := &Stats{}
	err := q.db.QueryRow(`SELECT COUNT(*) FROM tasks WHERE state = ?`, TaskPending).Scan(&stats.Pending)
	if err != nil {
		return nil, err
	}

	err = q.db.QueryRow(`SELECT COUNT(*) FROM tasks WHERE state = ?`, TaskRunning).Scan(&stats.Running)
	if err != nil {
		return nil, err
	}

	err = q.db.QueryRow(`SELECT COUNT(*) FROM tasks WHERE state = ?`, TaskCompleted).Scan(&stats.Completed)
	if err != nil {
		return nil, err
	}

	err = q.db.QueryRow(`SELECT COUNT(*) FROM tasks WHERE state = ?`, TaskFailed).Scan(&stats.Failed)
	if err != nil {
		return nil, err
	}

	stats.Total = stats.Pending + stats.Running + stats.Completed + stats.Failed
	return stats, nil
}

// initSchema 初始化数据库 schema。
func initSchema(db *sql.DB) error {
	_, err := db.Exec(`
		CREATE TABLE IF NOT EXISTS tasks (
			id TEXT PRIMARY KEY,
			goal TEXT NOT NULL,
			mode TEXT DEFAULT 'auto',
			state TEXT NOT NULL DEFAULT 'pending',
			priority INTEGER DEFAULT 0,
			created_at DATETIME NOT NULL,
			started_at DATETIME,
			ended_at DATETIME,
			error TEXT,
			result TEXT
		);

		CREATE INDEX IF NOT EXISTS idx_tasks_state ON tasks(state);
		CREATE INDEX IF NOT EXISTS idx_tasks_priority ON tasks(priority DESC, created_at ASC);
	`)
	return err
}
