package store

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	pgxuuid "github.com/vgarvardt/pgx-google-uuid/v5"
)

// New is the entry point main.go calls. When databaseURL is non-empty
// we dial Postgres, run migrations (idempotent — drop-policy/create-
// policy + create-if-not-exists are the entire shape) and return a
// pgx-backed Store. When it's empty (local dev without postgres) we
// return a memory-backed Store that satisfies the same interfaces.
//
// migrationsDir may be empty in which case we skip the boot-time
// migration step (useful for tests that seed schema themselves).
func New(ctx context.Context, databaseURL, migrationsDir string) (*Store, error) {
	if strings.TrimSpace(databaseURL) == "" {
		slog.Info("store: DATABASE_URL empty — using in-memory adapter (data lost on restart)")
		return NewMemory(), nil
	}

	cfg, err := pgxpool.ParseConfig(databaseURL)
	if err != nil {
		return nil, fmt.Errorf("pgxpool parse: %w", err)
	}
	// Reasonable defaults: 10 connections handles per-request fan-out
	// for an MVP comfortably; production should env-override based on
	// observed pool wait time. 30s acquire timeout protects against
	// the orchestrator hanging when Postgres is wedged.
	if cfg.MaxConns < 10 {
		cfg.MaxConns = 10
	}
	cfg.MaxConnLifetime = 30 * time.Minute
	cfg.MaxConnIdleTime = 5 * time.Minute

	// Register google/uuid codec on every new connection so the store
	// implementations can Scan into *uuid.UUID directly. pgx v5 doesn't
	// auto-recognise google/uuid — without this every SELECT on a uuid
	// column blows up with "cannot decode … into *uuid.UUID". The
	// vgarvardt/pgx-google-uuid package is the canonical 50-LOC helper
	// for this; no upstream support is on the roadmap.
	cfg.AfterConnect = func(ctx context.Context, conn *pgx.Conn) error {
		pgxuuid.Register(conn.TypeMap())
		return nil
	}

	dialCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	pool, err := pgxpool.NewWithConfig(dialCtx, cfg)
	if err != nil {
		return nil, fmt.Errorf("pgxpool connect: %w", err)
	}
	if err := pool.Ping(dialCtx); err != nil {
		pool.Close()
		return nil, fmt.Errorf("postgres ping: %w", err)
	}
	slog.Info("store: postgres pool ready", "max_conns", cfg.MaxConns)

	if migrationsDir != "" {
		if err := applyMigrations(ctx, pool, migrationsDir); err != nil {
			pool.Close()
			return nil, fmt.Errorf("apply migrations: %w", err)
		}
	}

	return &Store{
		Users:           &pgxUsers{pool: pool},
		Workspaces:      &pgxWorkspaces{pool: pool},
		SlideJobs:       &pgxSlideJobs{pool: pool},
		GameJobs:        &pgxGameJobs{pool: pool},
		VideoRuns:       &pgxVideoRuns{pool: pool},
		DesignAssets:    &pgxDesignAssets{pool: pool},
		IdempotencyKeys: &pgxIdempotencyKeys{pool: pool},
		closer:          func() error { pool.Close(); return nil },
	}, nil
}

// applyMigrations runs every *.sql file under dir in lexical order.
// We track applied filenames in a `schema_migrations` table so reruns
// are idempotent. This is intentionally simpler than goose / atlas —
// fewer moving parts at the cost of no down-migrations (a tradeoff
// that fits an early-stage product where rollback = restore from
// backup, not run-the-down-step).
func applyMigrations(ctx context.Context, pool *pgxpool.Pool, dir string) error {
	if _, err := pool.Exec(ctx, `
		create table if not exists schema_migrations (
			filename text primary key,
			applied_at timestamptz not null default now()
		)
	`); err != nil {
		return fmt.Errorf("create schema_migrations: %w", err)
	}

	entries, err := os.ReadDir(dir)
	if err != nil {
		return fmt.Errorf("read migrations dir %s: %w", dir, err)
	}
	files := make([]string, 0, len(entries))
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		if !strings.HasSuffix(e.Name(), ".sql") {
			continue
		}
		files = append(files, e.Name())
	}
	sort.Strings(files)

	for _, name := range files {
		var applied bool
		if err := pool.QueryRow(ctx,
			`select exists (select 1 from schema_migrations where filename = $1)`,
			name,
		).Scan(&applied); err != nil {
			return fmt.Errorf("check migration %s: %w", name, err)
		}
		if applied {
			continue
		}
		sql, err := os.ReadFile(filepath.Join(dir, name))
		if err != nil {
			return fmt.Errorf("read migration %s: %w", name, err)
		}
		// Each migration runs in a single transaction so a partial
		// failure rolls back cleanly. The migration file itself must
		// be tolerant of being half-run (e.g. our drop-policy +
		// create-policy idiom).
		tx, err := pool.Begin(ctx)
		if err != nil {
			return fmt.Errorf("begin migration %s: %w", name, err)
		}
		if _, err := tx.Exec(ctx, string(sql)); err != nil {
			_ = tx.Rollback(ctx)
			return fmt.Errorf("exec migration %s: %w", name, err)
		}
		if _, err := tx.Exec(ctx,
			`insert into schema_migrations (filename) values ($1)`, name,
		); err != nil {
			_ = tx.Rollback(ctx)
			return fmt.Errorf("record migration %s: %w", name, err)
		}
		if err := tx.Commit(ctx); err != nil {
			return fmt.Errorf("commit migration %s: %w", name, err)
		}
		slog.Info("store: migration applied", "file", name)
	}
	return nil
}
