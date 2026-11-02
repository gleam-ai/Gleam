package daemon

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"

	migrations "gleam/internal/harness/migrations"
	harness "gleam/internal/harness"

	_ "modernc.org/sqlite"
)

type TaskState string

const (
	TaskPending   TaskState = "pending"
	TaskRunning   TaskState = "running"
	TaskCompleted TaskState = "completed"
	TaskFailed    TaskState = "failed"
	TaskCancelled TaskState = "cancelled"
)

type QueuedTask struct {
	ID        string     `json:"id"`
	Goal      string     `json:"goal"`
	Mode      string     `json:"mode"`
	State     TaskState  `json:"state"`
	Priority  int        `json:"priority"` // 0=normal, 1=high, 2=urgent
	CreatedAt time.Time  `json:"created_at"`
	StartedAt *time.Time `json:"started_at,omitempty"`
	EndedAt   *time.Time `json:"ended_at,omitempty"`
	Error     string     `json:"error,omitempty"`
	Result    string     `json:"result,omitempty"` // marshalled types.GoalResult JSON
}

type Queue struct {
	mu sync.Mutex
	db *sql.DB
}

func OpenQueue(dataDir string) (*Queue, error) {
	if err := os.MkdirAll(dataDir, 0o755); err != nil {
		return nil, err
	}

	dbPath := filepath.Join(dataDir, "daemon_queue.db")
	db, err := sql.Open("sqlite", dbPath+"?_journal_mode=WAL&_busy_timeout=5000")
	if err != nil {
		return nil, fmt.Errorf("打开任务队列失败：%w", err)
	}

	// 初始化迁移并确保 schema 是最新的
	migrator, err := migrations.NewMigrator(db)
	if err != nil {
		db.Close()
		return nil, fmt.Errorf("创建迁移器失败：%w", err)
	}
	if err := migrator.Ensure(context.Background()); err != nil {
		db.Close()
		return nil, fmt.Errorf("运行迁移失败：%w", err)
	}

	return &Queue{db: db}, nil
}

func (q *Queue) Close() error {
	return q.db.Close()
}

// EnqueueTask executes task insertion within a transaction.
func (q *Queue) EnqueueTask(ctx context.Context, task *QueuedTask) error {
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

	_, err := harness.WithTx(ctx, q.db, func(ctx context.Context, tx *sql.Tx) error {
		_, err := tx.Exec(`
			INSERT INTO tasks (id, goal, mode, state, priority, created_at)
			VALUES (?, ?, ?, ?, ?, ?)
		`, task.ID, task.Goal, task.Mode, task.State, task.Priority, task.CreatedAt)
		return err
	})
	return err
}

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
		return nil, nil
	}
	if err != nil {
		return nil, err
	}

	return task, nil
}

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
	ctx := context.Background()
	_, err := harness.WithTx(ctx, q.db, func(ctx context.Context, tx *sql.Tx) error {
		_, err := tx.Exec(`
			UPDATE tasks
			SET state = ?, ended_at = ?, result = ?
			WHERE id = ?
		`, TaskCompleted, now, resultJSON, id)
		return err
	})
	return err
}

func (q *Queue) Fail(id string, errMsg string) error {
	q.mu.Lock()
	defer q.mu.Unlock()

	ctx := context.Background()
	_, err := harness.WithTx(ctx, q.db, func(ctx context.Context, tx *sql.Tx) error {
		now := time.Now()
		_, err := tx.Exec(`
			UPDATE tasks
			SET state = ?, ended_at = ?, error = ?
			WHERE id = ?
		`, TaskFailed, now, errMsg, id)
		return err
	})
	return err
}

func (q *Queue) Cancel(id string) error {
	q.mu.Lock()
	defer q.mu.Unlock()

	ctx := context.Background()
	_, err := harness.WithTx(ctx, q.db, func(ctx context.Context, tx *sql.Tx) error {
		_, err := tx.Exec(`
			UPDATE tasks
			SET state = ?, ended_at = ?
			WHERE id = ? AND state IN (?, ?)
		`, TaskCancelled, time.Now(), id, TaskPending, TaskRunning)
		return err
	})
	return err
}

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

func (q *Queue) RecoverPending() (int, error) {
	q.mu.Lock()
	defer q.mu.Unlock()

	ctx := context.Background()
	_, err := harness.WithTx(ctx, q.db, func(ctx context.Context, tx *sql.Tx) error {
		_, err := tx.Exec(`
			UPDATE tasks
			SET state = ?, started_at = NULL
			WHERE state = ?
		`, TaskPending, TaskRunning)
		return err
	})
	return 0, err
}

func (q *Queue) CleanupOld(keepDays int) (int, error) {
	q.mu.Lock()
	defer q.mu.Unlock()

	ctx := context.Background()
	var affected int64
	_, err := harness.WithTx(ctx, q.db, func(ctx context.Context, tx *sql.Tx) error {
		cutoff := time.Now().AddDate(0, 0, -keepDays)
		result, err := tx.Exec(`
			DELETE FROM tasks
			WHERE state IN (?, ?, ?) AND ended_at < ?
		`, TaskCompleted, TaskFailed, TaskCancelled, cutoff)
		if err != nil {
			return err
		}
		affectedRows, _ := result.RowsAffected()
		affected = affectedRows
		return nil
	})
	return int(affected), err
}

type Stats struct {
	Pending   int `json:"pending"`
	Running   int `json:"running"`
	Completed int `json:"completed"`
	Failed    int `json:"failed"`
	Total     int `json:"total"`
}

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
