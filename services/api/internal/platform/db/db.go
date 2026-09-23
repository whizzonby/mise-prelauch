// Package db owns the PostgreSQL connection pool and transaction helper.
package db

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/jackc/pgx/v5/stdlib"
	"github.com/pressly/goose/v3"

	"mise.tt/api/migrations"
)

// Querier is satisfied by both the pool and a transaction, so repository
// functions can run inside or outside a transaction.
type Querier interface {
	Exec(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error)
	Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error)
	QueryRow(ctx context.Context, sql string, args ...any) pgx.Row
}

func Open(ctx context.Context, url string) (*pgxpool.Pool, error) {
	cfg, err := pgxpool.ParseConfig(url)
	if err != nil {
		return nil, fmt.Errorf("parse database url: %w", err)
	}
	cfg.MaxConnLifetime = 30 * time.Minute
	pool, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		return nil, fmt.Errorf("connect: %w", err)
	}
	pingCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	if err := pool.Ping(pingCtx); err != nil {
		pool.Close()
		return nil, fmt.Errorf("ping database: %w", err)
	}
	return pool, nil
}

// InTx runs fn in a transaction, committing if it returns nil and rolling back
// otherwise.
func InTx(ctx context.Context, pool *pgxpool.Pool, fn func(tx pgx.Tx) error) error {
	tx, err := pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if err := fn(tx); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

// IsUniqueViolation reports whether err is a unique-constraint failure on the
// named constraint (or any constraint when name is empty).
func IsUniqueViolation(err error, constraint string) bool {
	var pgErr *pgconn.PgError
	return errors.As(err, &pgErr) && pgErr.Code == "23505" && (constraint == "" || pgErr.ConstraintName == constraint)
}

func migrationDB(pool *pgxpool.Pool) (*sql.DB, error) {
	goose.SetBaseFS(migrations.FS)
	goose.SetLogger(goose.NopLogger())
	if err := goose.SetDialect("postgres"); err != nil {
		return nil, err
	}
	return stdlib.OpenDBFromPool(pool), nil
}

// Migrate applies all pending migrations embedded in the binary.
func Migrate(ctx context.Context, pool *pgxpool.Pool) error {
	sqlDB, err := migrationDB(pool)
	if err != nil {
		return err
	}
	defer sqlDB.Close()
	return goose.UpContext(ctx, sqlDB, ".")
}

// MigrationVersion returns the current schema version.
func MigrationVersion(ctx context.Context, pool *pgxpool.Pool) (int64, error) {
	sqlDB, err := migrationDB(pool)
	if err != nil {
		return 0, err
	}
	defer sqlDB.Close()
	return goose.GetDBVersionContext(ctx, sqlDB)
}

// MigrateDown rolls back the most recent migration. Local and test use only.
func MigrateDown(ctx context.Context, pool *pgxpool.Pool) error {
	sqlDB, err := migrationDB(pool)
	if err != nil {
		return err
	}
	defer sqlDB.Close()
	return goose.DownContext(ctx, sqlDB, ".")
}
