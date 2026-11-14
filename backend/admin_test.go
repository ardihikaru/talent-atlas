package main

import (
	"context"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestEnsureAdminIsIdempotent(t *testing.T) {
	fake := &fakeArcade{migrated: map[string]bool{}}
	arcade := httptest.NewServer(fake.handler())
	defer arcade.Close()
	db := NewDB(arcade.URL, "education", "root", "secret")
	ctx, now := context.Background(), time.Now()

	if err := EnsureAdmin(ctx, db, "", "", now); err != nil || fake.users != 0 {
		t.Fatalf("empty username must be a no-op: err=%v users=%d", err, fake.users)
	}
	if err := EnsureAdmin(ctx, db, "ardi", "short", now); err == nil {
		t.Fatal("short password must be rejected")
	}
	if err := EnsureAdmin(ctx, db, "Ardi", "first-password-123", now); err != nil {
		t.Fatal(err)
	}
	if fake.users != 1 || !CheckPassword(fake.userHash, "first-password-123") {
		t.Fatalf("admin not created: users=%d", fake.users)
	}
	created := fake.commands[len(fake.commands)-1]
	if p, _ := created["params"].(map[string]any); p["username"] != "ardi" {
		t.Fatalf("username not normalized: %v", p["username"])
	}
	// Same password: nothing written.
	before := len(fake.commands)
	if err := EnsureAdmin(ctx, db, "ardi", "first-password-123", now); err != nil {
		t.Fatal(err)
	}
	for _, c := range fake.commands[before:] {
		if cmd, _ := c["command"].(string); !strings.HasPrefix(cmd, "MATCH") || strings.Contains(cmd, "SET") {
			t.Fatalf("unexpected write on unchanged password: %s", cmd)
		}
	}
	// Changed password: re-hashed, still one user.
	if err := EnsureAdmin(ctx, db, "ardi", "second-password-456", now); err != nil {
		t.Fatal(err)
	}
	if fake.users != 1 || !CheckPassword(fake.userHash, "second-password-456") {
		t.Fatal("password not updated in place")
	}
}

func TestNormalizeUsername(t *testing.T) {
	for in, ok := range map[string]bool{"ardi": true, " Ardi ": true, "a.b-c_d": true, "ab": false, "has space": false, "x@y.z": false} {
		if _, err := NormalizeUsername(in); (err == nil) != ok {
			t.Errorf("%q: ok=%v err=%v", in, ok, err)
		}
	}
}
