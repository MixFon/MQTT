// Package migrate — самописный раннер миграций поверх database/sql.
// Читает .sql-файлы из embed.FS, применяет неприменённые по порядку имён,
// отмечая каждый в таблице schema_migrations.
package migrate

import (
	"context"
	"database/sql"
	"embed"
	"fmt"
	"io/fs"
	"sort"
	"strings"
)

// Run применяет все ещё не применённые миграции из migrationsFS к db,
// по одной, в порядке сортировки имён файлов.
func Run(ctx context.Context, db *sql.DB, migrationsFS embed.FS) error {
	if err := ensureSchemaMigrationsTable(ctx, db); err != nil {
		return fmt.Errorf("create schema_migrations table: %w", err)
	}

	entries, err := fs.ReadDir(migrationsFS, ".")
	if err != nil {
		return fmt.Errorf("read migrations dir: %w", err)
	}

	var names []string
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".sql") {
			continue
		}
		names = append(names, e.Name())
	}
	sort.Strings(names)

	for _, name := range names {
		applied, err := isApplied(ctx, db, name)
		if err != nil {
			return fmt.Errorf("check migration %s: %w", name, err)
		}
		if applied {
			continue
		}

		sqlBytes, err := migrationsFS.ReadFile(name)
		if err != nil {
			return fmt.Errorf("read migration %s: %w", name, err)
		}

		if err := applyMigration(ctx, db, name, string(sqlBytes)); err != nil {
			return fmt.Errorf("apply migration %s: %w", name, err)
		}
	}

	return nil
}

// ensureSchemaMigrationsTable создаёт таблицу schema_migrations, если её ещё нет —
// в ней хранятся имена уже применённых файлов миграций.
func ensureSchemaMigrationsTable(ctx context.Context, db *sql.DB) error {
	const stmt = `
CREATE TABLE IF NOT EXISTS schema_migrations (
	version    TEXT PRIMARY KEY,
	applied_at TIMESTAMPTZ NOT NULL DEFAULT now()
)`
	_, err := db.ExecContext(ctx, stmt)
	return err
}

// isApplied сообщает, отмечена ли миграция version как уже применённая
// в таблице schema_migrations.
func isApplied(ctx context.Context, db *sql.DB, version string) (bool, error) {
	var exists bool
	err := db.QueryRowContext(ctx,
		`SELECT EXISTS(SELECT 1 FROM schema_migrations WHERE version = $1)`, version,
	).Scan(&exists)
	return exists, err
}

// applyMigration выполняет SQL миграции и запись в schema_migrations
// одной транзакцией — чтобы при сбое применения не остался "наполовину применённый" файл.
func applyMigration(ctx context.Context, db *sql.DB, version, sqlText string) error {
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	if _, err := tx.ExecContext(ctx, sqlText); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO schema_migrations (version) VALUES ($1)`, version); err != nil {
		return err
	}
	return tx.Commit()
}
