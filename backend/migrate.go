package main

import (
	"context"
	"embed"
	"fmt"
	"log"
	"sort"
	"strings"
)

//go:embed migrations/*.sql
var migrationFiles embed.FS

// Migrate applies every embedded migration that has not run yet, in name order.
func Migrate(ctx context.Context, db *DB) error {
	if _, err := db.Run(ctx, SQL("CREATE DOCUMENT TYPE SchemaMigration IF NOT EXISTS", nil), ""); err != nil {
		return fmt.Errorf("prepare migration table: %w", err)
	}
	entries, err := migrationFiles.ReadDir("migrations")
	if err != nil {
		return err
	}
	names := make([]string, 0, len(entries))
	for _, e := range entries {
		names = append(names, e.Name())
	}
	sort.Strings(names)
	for _, name := range names {
		version := strings.TrimSuffix(name, ".sql")
		done, err := migrationApplied(ctx, db, version)
		if err != nil {
			return err
		}
		if done {
			continue
		}
		raw, err := migrationFiles.ReadFile("migrations/" + name)
		if err != nil {
			return err
		}
		for _, statement := range splitSQL(string(raw)) {
			if _, err := db.Run(ctx, SQL(statement, nil), ""); err != nil {
				return fmt.Errorf("migration %s: %q: %w", version, statement, err)
			}
		}
		if err := markMigration(ctx, db, version); err != nil {
			return err
		}
		log.Printf("migration applied: %s", version)
	}
	return nil
}

func migrationApplied(ctx context.Context, db *DB, version string) (bool, error) {
	n, err := db.Count(ctx, SQL("SELECT count(*) AS n FROM SchemaMigration WHERE version = :version",
		map[string]any{"version": version}))
	return n > 0, err
}

func markMigration(ctx context.Context, db *DB, version string) error {
	_, err := db.Run(ctx, SQL("INSERT INTO SchemaMigration SET version = :version, appliedAt = sysdate()",
		map[string]any{"version": version}), "")
	return err
}

// splitSQL returns the statements of a file, dropping blank lines and -- comments.
func splitSQL(source string) []string {
	var cleaned []string
	for _, line := range strings.Split(source, "\n") {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" || strings.HasPrefix(trimmed, "--") {
			continue
		}
		cleaned = append(cleaned, trimmed)
	}
	var out []string
	for _, part := range strings.Split(strings.Join(cleaned, " "), ";") {
		if s := strings.TrimSpace(part); s != "" {
			out = append(out, s)
		}
	}
	return out
}
