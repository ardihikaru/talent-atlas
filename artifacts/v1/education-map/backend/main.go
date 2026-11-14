// Command server is the starter backend of the education map: it prepares
// ArcadeDB (database, schema, catalog), then serves the onboarding wizard and its API.
package main

import (
	"context"
	"crypto/rand"
	"errors"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"
)

func env(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

func main() {
	log.SetFlags(log.LstdFlags | log.LUTC)
	db := NewDB(
		env("ARCADEDB_URL", "http://localhost:2480"),
		env("ARCADEDB_DATABASE", "education"),
		env("ARCADEDB_USER", "root"),
		os.Getenv("ARCADEDB_PASSWORD"),
	)
	secret := []byte(os.Getenv("SESSION_SECRET"))
	if len(secret) < 32 {
		log.Print("SESSION_SECRET is missing or shorter than 32 characters: using a random one, so sign-ins end when the server restarts")
		secret = make([]byte, 32)
		if _, err := rand.Read(secret); err != nil {
			log.Fatal(err)
		}
	}

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	startup, cancel := context.WithTimeout(ctx, 5*time.Minute)
	log.Printf("waiting for ArcadeDB at %s", db.BaseURL)
	if err := db.WaitReady(startup); err != nil {
		log.Fatal(err)
	}
	if err := db.EnsureDatabase(startup); err != nil {
		log.Fatal(err)
	}
	if err := Migrate(startup, db); err != nil {
		log.Fatal(err)
	}
	if err := Seed(startup, db); err != nil {
		log.Fatal(err)
	}
	cancel()

	app := &Server{DB: db, Sessions: Sessions{Secret: secret, Secure: env("COOKIE_SECURE", "false") == "true"}, Now: time.Now}
	server := &http.Server{
		Addr:              ":" + env("PORT", "8080"),
		Handler:           app.Routes(),
		ReadHeaderTimeout: 10 * time.Second,
		ReadTimeout:       30 * time.Second,
		WriteTimeout:      60 * time.Second,
		IdleTimeout:       120 * time.Second,
	}
	go func() {
		<-ctx.Done()
		shutdown, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		_ = server.Shutdown(shutdown)
	}()
	log.Printf("listening on %s (database %q)", server.Addr, db.Database)
	if err := server.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		log.Fatal(err)
	}
}
