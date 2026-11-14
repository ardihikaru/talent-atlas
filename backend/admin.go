package main

import (
	"context"
	"errors"
	"fmt"
	"log"
	"regexp"
	"strings"
	"time"
)

var usernamePattern = regexp.MustCompile(`^[a-z0-9][a-z0-9._-]{2,39}$`)

// NormalizeUsername lowercases and trims a username and checks its shape.
func NormalizeUsername(raw string) (string, error) {
	u := strings.ToLower(strings.TrimSpace(raw))
	if !usernamePattern.MatchString(u) {
		return "", errors.New("username must be 3 to 40 characters: letters, digits, dot, dash or underscore")
	}
	return u, nil
}

// EnsureAdmin makes sure the account named by ADMIN_USERNAME exists with ADMIN_PASSWORD.
// It creates the owner when missing and re-hashes the password only when it changed,
// so it is safe to run on every boot. Empty username means: do nothing.
func EnsureAdmin(ctx context.Context, db *DB, rawUsername, password string, now time.Time) error {
	if rawUsername == "" {
		return nil
	}
	username, err := NormalizeUsername(rawUsername)
	if err != nil {
		return fmt.Errorf("ADMIN_USERNAME: %w", err)
	}
	if len(password) < 10 || len(password) > 200 {
		return errors.New("ADMIN_PASSWORD must be 10 to 200 characters")
	}
	rows, err := db.Run(ctx, Cypher(
		"MATCH (u:User {username: $username}) RETURN u.id AS id, u.passwordHash AS hash",
		map[string]any{"username": username}), "")
	if err != nil {
		return err
	}
	if len(rows) > 0 {
		id, _ := rows[0]["id"].(string)
		hash, _ := rows[0]["hash"].(string)
		if CheckPassword(hash, password) {
			log.Printf("admin account ready: %s", username)
			return nil
		}
		newHash, err := HashPassword(password)
		if err != nil {
			return err
		}
		if _, err := db.Run(ctx, Cypher("MATCH (u:User {id: $id}) SET u.passwordHash = $hash",
			map[string]any{"id": id, "hash": newHash}), ""); err != nil {
			return err
		}
		log.Printf("admin password updated: %s", username)
		return nil
	}
	hash, err := HashPassword(password)
	if err != nil {
		return err
	}
	if _, err := db.Run(ctx, Cypher(
		"CREATE (u:User {id: $id, username: $username, passwordHash: $hash, role: 'owner', createdAt: $now})",
		map[string]any{"id": NewID("user"), "username": username, "hash": hash, "now": now.UTC().Format(time.RFC3339)}), ""); err != nil {
		return err
	}
	log.Printf("admin account created: %s", username)
	return nil
}
