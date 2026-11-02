package harness

import (
	"context"
	"database/sql"
	"embed"
	"fmt"
	"os"
	"sort"
	"strconv"
	"strings"

	_ "modernc.org/sqlite"
)

//go:embed *.sql
var migrationFS embed.FS

type Migrator struct {
	db        *sql.DB
	migrations []migration
}

type migration struct {
	id      int
	upFile  string
	downFile string
}

func NewMigrator(db *sql.DB) (*Migrator, error) {
	m := &Migrator{db: db}
	if err := m.discoverMigrations(); err != nil {
		return nil, fmt.Errorf("discover migrations: %w", err)
	}
	sort.Slice(m.migrations, func(i, j int) bool { return m.migrations[i].id < m.migrations[j].id })
	return m, nil
}

func (m *Migrator) discoverMigrations() error {
	files, err := migrationFS.ReadDir(".")
	if err != nil {
		return fmt.Errorf("read migrations dir: %w", err)
	}
	var pending []migration
	for _, f := range files {
		name := f.Name()
		if !strings.HasSuffix(name, "_up.sql") && !strings.HasSuffix(name, "_down.sql") {
			continue
		}
		base := strings.TrimSuffix(name, "_up.sql")
		if base == strings.TrimSuffix(strings.TrimSuffix(name, "_up.sql"), "_down.sql") {
			pending = append(pending, migration{id: parseID(base), upFile: name})
		} else {
			for i := range pending {
				if pending[i].id == parseID(base) {
					pending[i].downFile = name
				}
			}
		}
	}
	for _, pm := range pending {
		if pm.downFile == "" {
			fmt.Fprintf(os.Stderr, "warning: no down migration for %s\n", pm.upFile)
		}
		m.migrations = append(m.migrations, pm)
	}
	return nil
}

func parseID(s string) int {
	parts := strings.Split(s, "_")
	if len(parts) == 0 {
		return 0
	}
	i, _ := strconv.Atoi(parts[0])
	return i
}

func (m *Migrator) Ensure(ctx context.Context) error {
	if err := m.createSchemaTable(ctx); err != nil {
		return err
	}
	currentVersion, err := m.currentVersion(ctx)
	if err != nil {
		return err
	}
	for _, mig := range m.migrations {
		if mig.id <= currentVersion {
			continue
		}
		if err := m.applyMigration(ctx, mig, true); err != nil {
			return fmt.Errorf("apply migration %s: %w", mig.upFile, err)
		}
		if err := m.setVersion(ctx, mig.id); err != nil {
			return fmt.Errorf("set version after %s: %w", mig.upFile, err)
		}
	}
	return nil
}

func (m *Migrator) RollbackTo(ctx context.Context, version int) error {
	for i := len(m.migrations) - 1; i >= 0; i-- {
		mig := m.migrations[i]
		if mig.id <= version {
			break
		}
		if err := m.applyMigration(ctx, mig, false); err != nil {
			return fmt.Errorf("rollback migration %s: %w", mig.downFile, err)
		}
		if err := m.setVersion(ctx, mig.id-1); err != nil {
			return fmt.Errorf("set version after rollback: %w", err)
		}
	}
	return nil
}

func (m *Migrator) applyMigration(ctx context.Context, mig migration, up bool) error {
	var content string
	var err error
	if up {
		content, err = readMigration(mig.upFile)
	} else {
		content, err = readMigration(mig.downFile)
	}
	if err != nil {
		return err
	}
	_, err = m.db.ExecContext(ctx, content)
	return err
}

func readMigration(name string) (string, error) {
	data, err := migrationFS.ReadFile(name)
	if err != nil {
		return "", err
	}
	return string(data), nil
}

func (m *Migrator) createSchemaTable(ctx context.Context) error {
	_, err := m.db.ExecContext(ctx, `
		CREATE TABLE IF NOT EXISTS schema_migrations (
			version INTEGER PRIMARY KEY,
			applied_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
		)
	`)
	return err
}

func (m *Migrator) currentVersion(ctx context.Context) (int, error) {
	var v sql.NullInt64
	err := m.db.QueryRowContext(ctx, "SELECT COALESCE(MAX(version), 0) FROM schema_migrations").Scan(&v)
	if err != nil {
		return 0, err
	}
	if !v.Valid {
		return 0, nil
	}
	return int(v.Int64), nil
}

func (m *Migrator) setVersion(ctx context.Context, version int) error {
	_, err := m.db.ExecContext(ctx, "INSERT INTO schema_migrations (version) VALUES ($1)", version)
	return err
}
