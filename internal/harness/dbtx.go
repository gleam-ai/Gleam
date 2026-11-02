package harness

import (
	"context"
	"database/sql"
	"fmt"

	"github.com/google/uuid"
)

// WithTx runs fn within a transaction. On any error, it rolls back; on success, commits.
// It returns the traceID for observability.
func WithTx(ctx context.Context, db *sql.DB, fn func(ctx context.Context, tx *sql.Tx) error) (string, error) {
	traceID := uuid.New().String()
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return traceID, fmt.Errorf("begin tx: %w", err)
	}
	defer func() {
		if p := recover(); p != nil {
			_ = tx.Rollback()
			panic(p)
		}
	}()

	err = fn(ctx, tx)
	if err != nil {
		_ = tx.Rollback()
		return traceID, err
	}

	if commitErr := tx.Commit(); commitErr != nil {
		return traceID, fmt.Errorf("commit tx: %w", commitErr)
	}
	return traceID, nil
}
